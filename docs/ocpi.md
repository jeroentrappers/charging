# OCPI eMSP: integrating a CPO directly

Besides aggregator feeds, the app can act as an **OCPI 2.2.1 eMSP** (a data
consumer) and connect directly to a CPO — pulling Locations + Tariffs and
receiving real-time pushes. This is `internal/ocpi` (client `Register` + the
eMSP `Server`) wired into the API.

Our party identity (configurable): `OCPI_COUNTRY=BE`, `OCPI_PARTY_ID=APM`,
`OCPI_PARTY_NAME=Appmire Charging`.

## Endpoints we expose

Served under `/ocpi` (publicly `https://charging.appmire.be/api/ocpi/…`; set
`PUBLIC_URL` so advertised URLs are absolute). All require the CPO's token.

- `GET /ocpi/versions` → our versions list (2.2.1)
- `GET /ocpi/2.2.1` → our endpoints (credentials + locations/tariffs **RECEIVER**)
- `GET|POST|DELETE /ocpi/2.2.1/credentials`
- `PUT|PATCH|DELETE /ocpi/2.2.1/{locations,tariffs}/…` — the CPO pushes here

## The handshake (eMSP-initiated)

The CPO shares, out of band, **their versions URL** and a one-time **Token A**.
Then:

```bash
curl -X POST https://charging.appmire.be/api/admin/sources/<id>/ocpi/register \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"versions_url":"<their /ocpi/versions>","token":"<Token A>"}'
```

This (`ocpi.Register`): discovers their version + endpoints, generates **Token B**
(stored in `cpo.ocpi_token_in`; the CPO presents it to push to us), POSTs our
credentials to their `credentials` endpoint, and receives **Token C** (stored in
`cpo.token`; we use it to call them). The source is set to `source_type=ocpi`,
enabled, with their version-details URL as the pull base — so the scheduler then
**pulls** Locations + Tariffs on its normal cadence.

Token transport: OCPI 2.2+ base64-encodes the token; 2.1.1 sends it raw (handled
both ways).

## Push receiver

After registration the CPO can `PUT/PATCH` Location and Tariff objects to our
receiver endpoints (authenticated with Token B). Pushed objects flow through the
same `engine.IngestOCPI` path as pulled data (upsert chargers + SCD2 tariff
versions), so they're priced, plug-normalized and deduped identically. A small
per-CPO in-memory cache resolves a connector's tariff regardless of push order.
**Periodic pull stays authoritative**; push provides real-time deltas between
pulls. (Sessions/CDRs modules aren't implemented — not needed for price
comparison.)

## Reality check

OCPI is **bilateral**, not an open API: connecting to an operator needs them to
provision credentials (a roaming/data agreement) or a roaming hub
(Hubject/Gireve). That's why the open NAP/aggregator route (Road, Monta,
Eco-Movement) is the coverage backbone; direct OCPI is for specific partners.

> Validated against a mock CPO end-to-end (handshake + push → priced charger).
> The first real handshake may surface a vendor quirk to iron out.
