---
title: Custom domains
slug: custom-domains
group: Hosting
weight: 32
---

A site on a network lives at `my-site.friendo.world`. You can point a domain you
own at it instead — `mysite.com` — and keep the network address too.

## Connect it

From the folder of the site (or with `--site my-site`):

```bash
friendo domain add mysite.com
```

The network records the domain for your site and prints the DNS records to add
with whoever you bought the domain from — on friendo.world that's a single
`CNAME`. Nothing changes yet: **an unverified domain never serves traffic**, which
is what stops someone connecting a domain that isn't theirs.

The same thing, in a browser: open your [account page](/docs/network-account),
press **Domains** on the site, and enter the domain. The DNS records appear right
there.

## Go live

Add the records, wait for DNS to travel (minutes, occasionally hours), then:

```bash
friendo domain verify mysite.com
```

The network checks the records (on friendo.world, Cloudflare also issues the
certificate) and the domain starts serving your site over HTTPS. If it's not
there yet, the command says so — try again later. Until then, anyone who visits
`mysite.com` sees a short page explaining the domain isn't live yet and what's
left to do, so you can check on it from a browser as well.

## See and disconnect

```bash
friendo domain list              # your domains and whether each is live
friendo domain remove mysite.com # disconnect — the site keeps its network address
```

## For network operators

A self-hosted network verifies a `TXT` record itself and leaves TLS to whatever
is in front of it (Traefik, Caddy, a Cloudflare zone). With Cloudflare for SaaS
configured, verification and certificates are handled by Cloudflare and the
tenant adds one `CNAME`. Either way, the proxy in front has to pass unknown
hostnames through to friendo. Details in [Run a network](/docs/run-a-network).
Operators can also connect, check and disconnect a domain on any site from the
network console.
