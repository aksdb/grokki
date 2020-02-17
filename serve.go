package main

import (
	"context"
	"fmt"
	"github.com/gliderlabs/ssh"
	"go.uber.org/zap"
	gossh "golang.org/x/crypto/ssh"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"
)

type ServeCmd struct {
	HostKey           string `help:"Key file to use for the ssh server." default:".serverkey"`
	SshListenAddress  string `help:"Address to listen on for ssh connection." default:":2222"`
	HttpListenAddress string `help:"Address to listen on for the http (proxy) connections." default:":2491"`
	BaseDomain        string `help:"Domain to use as a base." required:""`
}

func (r *ServeCmd) Run() error {
	zap.L().Info("Serving")
	sshForwardHandler := &ForwardedTCPHandler{}

	ssh.Handle(func(session ssh.Session) {
		EnsureSessionInfo(session.Context())

		_, _ = io.Copy(ioutil.Discard, session)
		session.Exit(0)
	})
	ssh.DefaultRequestHandlers["tcpip-forward"] = sshForwardHandler.HandleSSHRequest
	ssh.DefaultRequestHandlers["cancel-tcpip-forward"] = sshForwardHandler.HandleSSHRequest
	go func() {
		zap.L().Info("Listening for SSH connections.", zap.String("listenAddress", cli.Serve.SshListenAddress))
		if err := ssh.ListenAndServe(cli.Serve.SshListenAddress, nil, ssh.HostKeyFile(cli.Serve.HostKey)); err != nil {
			zap.L().Fatal("Cannot listen for SSH connections.", zap.Error(err))
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		proxyLock.Lock()
		proxy := activeProxies[host]
		proxyLock.Unlock()

		if proxy == nil {
			zap.L().Info("No proxy for domain.", zap.String("host", host))
			http.Error(w, "", http.StatusNotImplemented)
			return
		}

		proxy.ServeHTTP(w, r)
	})

	zap.L().Info("Listening for HTTP connections.", zap.String("listenAddress", cli.Serve.HttpListenAddress))
	return http.ListenAndServe(cli.Serve.HttpListenAddress, nil)
}

var proxyLock sync.Mutex
var activeProxies = make(map[string]*httputil.ReverseProxy)

var sessionLock sync.Mutex
var sessions = make(map[string]*SessionInfo)

type BindInfo struct {
	BindAddr string
	BindPort uint32
}

type SessionInfo struct {
	m           sync.Mutex
	activeBinds map[BindInfo]string
}

func (w *SessionInfo) AddBinding(info BindInfo, domain string) {
	w.m.Lock()
	defer w.m.Unlock()
	w.activeBinds[info] = domain
}

func (w *SessionInfo) RemoveBinding(info BindInfo) {
	w.m.Lock()
	defer w.m.Unlock()
	delete(w.activeBinds, info)
}

func (w *SessionInfo) GetBinding(info BindInfo) string {
	w.m.Lock()
	defer w.m.Unlock()
	return w.activeBinds[info]
}

func (w *SessionInfo) Cleanup() {
	w.m.Lock()
	proxyLock.Lock()
	for k, domain := range w.activeBinds {
		zap.L().Info("Removing domain binding.", zap.String("domain", domain))
		delete(activeProxies, domain)
		delete(w.activeBinds, k)
	}
	w.m.Unlock()
	proxyLock.Unlock()
}

func EnsureSessionInfo(ctx context.Context) *SessionInfo {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	sessionId := ctx.Value(ssh.ContextKeySessionID).(string)
	sessionInfo := sessions[sessionId]
	if sessionInfo == nil {
		zap.L().Debug("Creating session.", zap.String("sessionId", sessionId))
		sessionInfo = &SessionInfo{
			activeBinds: make(map[BindInfo]string),
		}
		sessions[sessionId] = sessionInfo

		go func() {
			<-ctx.Done()

			zap.L().Debug("Closing session.", zap.String("sessionId", sessionId))
			sessionLock.Lock()
			defer sessionLock.Unlock()
			delete(sessions, sessionId)
			sessionInfo.Cleanup()
		}()
	}
	return sessionInfo
}

type ForwardedTCPHandler struct {
}

func (h *ForwardedTCPHandler) HandleSSHRequest(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	sessionInfo := EnsureSessionInfo(ctx)

	conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
	switch req.Type {
	case "tcpip-forward":
		var reqPayload BindInfo
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			return false, []byte{}
		}

		// Get the lock early on, because we need to determine a domain name
		// which might need trial and error. Also we want to prevent to requests
		// at the same time for the same domain being allowed just due to bad timing.
		proxyLock.Lock()
		defer proxyLock.Unlock()

		subDomain := strings.TrimSpace(reqPayload.BindAddr)
		if subDomain == "localhost" {
			// SSH clients seem to set "localhost" by default.
			subDomain = ""
		}

		domain := buildDomainName(subDomain)

		if subDomain != "" && activeProxies[domain] != nil {
			zap.L().Warn("Request for domain denied, since the domain is already bound.", zap.String("domain", domain))
			return false, nil
		}

		if subDomain == "" {
			// Build random domain names until we find one that is not taken yet.
			for {
				subDomain = getRandomDomainName()
				domain = buildDomainName(subDomain)

				if activeProxies[domain] == nil {
					break
				}
			}
		}

		dialFunc := func(ctx context.Context, network, address string) (net.Conn, error) {
			payload := gossh.Marshal(&struct {
				DestAddr   string
				DestPort   uint32
				OriginAddr string
				OriginPort uint32
			}{
				DestAddr:   reqPayload.BindAddr,
				DestPort:   reqPayload.BindPort,
				OriginAddr: "localhost",
				OriginPort: 8080,
			})
			conn, reqs, err := conn.OpenChannel("forwarded-tcpip", payload)
			if err != nil {
				return nil, err
			}
			go gossh.DiscardRequests(reqs)
			return &connWrap{
				Channel:       conn,
				remoteAddress: dummyAddr(reqPayload.BindAddr),
			}, nil
		}

		proxy := &httputil.ReverseProxy{
			Director: func(r *http.Request) {
				r.URL.Scheme = "http"
				r.URL.Host = domain
			},
			Transport: &http.Transport{
				DialContext:           dialFunc,
				MaxIdleConns:          10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}

		zap.L().Info("Add domain binding.", zap.String("domain", domain))
		activeProxies[domain] = proxy
		sessionInfo.AddBinding(reqPayload, domain)

		return true, gossh.Marshal(&struct{ DestPort uint32 }{reqPayload.BindPort})

	case "cancel-tcpip-forward":
		var reqPayload BindInfo
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			return false, []byte{}
		}

		domain := sessionInfo.GetBinding(reqPayload)
		if domain == "" {
			zap.L().Warn("Cannot remove domain binding. Not found.")
		} else {
			zap.L().Info("Removing domain binding.", zap.String("domain", domain))
			proxyLock.Lock()
			delete(activeProxies, domain)
			proxyLock.Unlock()
			sessionInfo.RemoveBinding(reqPayload)
		}

		return true, nil
	default:
		return false, nil
	}
}

type connWrap struct {
	gossh.Channel
	remoteAddress net.Addr
}

func (c *connWrap) LocalAddr() net.Addr                { return dummyAddr("localhost") }
func (c *connWrap) RemoteAddr() net.Addr               { return c.remoteAddress }
func (c *connWrap) SetDeadline(t time.Time) error      { return fmt.Errorf("not implemented") }
func (c *connWrap) SetReadDeadline(t time.Time) error  { return fmt.Errorf("not implemented") }
func (c *connWrap) SetWriteDeadline(t time.Time) error { return fmt.Errorf("not implemented") }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }

const domainAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func getRandomDomainName() string {
	const length = 10
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = domainAlphabet[rand.Intn(len(domainAlphabet))]
	}
	return string(result)
}

func buildDomainName(subdomain string) string {
	return subdomain + "." + cli.Serve.BaseDomain
}
