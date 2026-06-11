# Frontend — UX analysis

What we're displaying and how to make it genuinely easy to use. The backend
already exposes everything needed (see API table in the README); this doc is the
design that sits on top.

## Who uses it, and the one job that matters

- **Primary — the driver, on mobile, often in the car, sometimes low battery.**
  Job-to-be-done: *"Where can I charge cheapest near me, right now, that's
  actually free to use, for the kind of charge I need?"* Minimal taps, glanceable.
- **Secondary — the planner, on desktop**, comparing options before a trip.
- **Tertiary — the curious / Appmire stakeholder**, who wants price *insights*
  (AC vs DC, trends, regional differences).

Everything below optimizes for the primary job; the rest are secondary views.

## The three things the data forces us to get right

1. **Price is not one number.** A tariff mixes €/kWh + €/min + session fees, so
   "cheapest" only means something for a *specific session*. The **session
   selector is the heart of the UI**, not a buried filter. Frame it in human
   terms, not profile keys:
   - *"What are you doing?"* → Quick top-up (~100 km) · Full charge (10→80%)
   - *"How fast?"* → Slow (AC 11) · Fast AC (22) · Rapid (DC 150) · Ultra (DC 300)
   These map to the `/sessions` profile keys and the `?session=` param. Default
   to **Full charge @ the charger's own speed** (the headline price) so the app
   works before the user touches anything.
2. **Availability must be honest.** Show **free now / in use / unknown** as a
   clear three-state badge. Never render stale data as "free" — the API already
   excludes stale under `available=true` and returns `availability_stale` +
   `status_updated_at`; surface "updated 3 min ago". A dead feed must look
   uncertain, not green.
3. **Ad-hoc price ≠ your card price.** A persistent, quiet caveat: *"Drive-up
   price, no subscription — your charge card may differ."* Builds trust; avoids
   "your app lied to me."

## Screens

### 1. Find (primary) — map + list, one screen
- **Map** (default to current location): markers colored on a **price scale**
  (green = cheap → red = pricey) for the selected session; a compact price label
  on each marker (e.g. "€0.42"). Availability shown by marker opacity/ring.
  Cluster markers when zoomed out.
- **List** (bottom sheet on mobile, side panel on desktop): ranked cheapest
  first — price (big), distance, power + plug, availability badge, operator.
  Tapping a row highlights the marker and vice-versa.
- **Controls (sticky, top):** location (geolocate + search), the session
  selector, and a filters affordance (min power, plug type, "only available",
  AC/DC). Radius is implicit from the map viewport.
- **States:** locating… / no chargers in view / none available (offer "show
  occupied too") / location denied (fall back to search) / load error.

### 2. Charger detail
- Name, operator, address, connectors (power, plug, current type).
- **Availability** with freshness ("free now · updated 2 min ago").
- **Price for the selected session**, plus the full **session matrix** table so
  power users see every scenario.
- **Price history chart** (`/chargers/{id}/price-history`) — the differentiator:
  "this charger went €0.45 → €0.55 on 12 May".
- Actions: navigate (open in maps), back to results.

### 3. Insights (secondary) — the statistics goal, surfaced
- Market overview: # chargers, avg/median price **AC vs DC** (`/stats/overview`).
- **Price trend** line chart over months (`/stats/price-trend`).
- Cheapest/priciest **regions** (`/stats/regions`).
- Per-session averages (`/stats/sessions`).

## API mapping & gaps

| Need | Endpoint | Status |
|---|---|---|
| Cheapest near me, by session | `GET /chargers/cheapest?lat&lon&radius&session&available&min_power&plug` | ✅ |
| Session selector options | `GET /sessions` | ✅ |
| Charger price history | `GET /chargers/{id}/price-history` | ✅ |
| Insights | `GET /stats/*` | ✅ |

**Gaps to add (small, backend):**
- **Bounding-box query** for the map: today's endpoint is radius-around-a-point;
  panning a rectangular map wants `bbox`. Radius-from-viewport-center is an
  acceptable v1 shim; add `bbox` + marker **clustering**/limit for dense cities.
- **Single-charger endpoint** `GET /chargers/{id}` for the detail view (today
  the info only comes back inside a cheapest result). Minor.
- **Geocoding** for location search isn't ours — use a public geocoder
  (e.g. Nominatim) client-side, or ship geolocate-only in v1.
- **CORS**: if the frontend is served from a different origin than the API, add
  permissive CORS on read endpoints. (Avoidable if the API serves the static SPA.)

## Cross-cutting

- **Mobile-first**: thumb-reachable controls, bottom-sheet list, big tap targets,
  one-handed. Desktop gets the side-by-side map+list.
- **Performance/perceived speed**: optimistic skeletons, debounce map-move
  refetch, cache `/sessions`.
- **Accessibility**: don't encode price by color alone (also label it);
  keyboard-navigable list; sufficient contrast.
- **Trust/attribution**: footer crediting transportdata.be / AFIR open data;
  the ad-hoc caveat; "availability as of" timestamps.
- **Empty-before-tokens reality**: until a real source is live the map is empty
  (or demo-seeded). Ship a friendly empty state and the demo seed for showcasing.
- **Settings & display prefs**: a Settings panel (gear icon) holds the car
  parameters + detour weighting (localStorage, drives the client-side ranking)
  and a **Display** section — language (en/nl/fr) and a Light/Dark/System theme.
  Theme is persisted (`charging.theme`), follows the OS live in System mode, and
  is applied before first paint by an inline script in `index.html` (no flash);
  dark mode also darkens the OSM tiles via a CSS filter on the tile pane.

## Proposed build (v1 scope)

The **Find** screen end-to-end (map + list + session selector + filters +
detail with history chart), mobile-first, against the live API. **Insights** as
a second tab once Find is solid. Keep it deployable alongside the Go API.
