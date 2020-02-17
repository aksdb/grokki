# Grokki

This service is basically a slim and naive version of ngrok,
based on the works of [Artyom Pervukhin (grok)](https://github.com/artyom/grok).

## Features (WIP)

* Create a remote port forward by using a default SSH client to forward local ports.
* Each individual forward is assigned to a subdomain.
* Subdomains can be random or freely chosen.
* Authentication via username/password or username/key.

## Later

* Administrative interface (either gui, tui or cui)
* Simple file server (needs some kind of client; ssh cannot forward a local directory on its own)

## Out of scope

* Scalability (I didn't design this with any kind of clustering in mind).
* High Availability (as I said: no clustering. if the service dies, connections are gone)

## (possibly) FAQ

### Why not fork grok?

* I want to expand the terminal/shell aspect.
* I don't want the service to deal with TLS (that's something caddy or traefik can do).

All in all I have a different design goal.

### Why not use localtunnel?

I wanted a solution that can work without any third party client. SSH is available
nearly everywhere, so using this as a starting point to initiate the port forward
seems sensible.

### Why not use ngrok?

You cannot self-host it. If you don't care about that, go ahead and use it.