import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// Installable PWA. The API lives on a separate origin (VITE_API_BASE); API and
// OSM tile responses are runtime-cached so the app stays fast and works briefly
// offline.
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['icon.svg'],
      manifest: {
        name: 'Charging — cheapest EV charger nearby',
        short_name: 'Charging',
        description: 'Find the cheapest available public EV charger near you.',
        theme_color: '#15803d',
        background_color: '#0b1220',
        display: 'standalone',
        start_url: '/',
        icons: [
          { src: 'icon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any maskable' },
        ],
      },
      workbox: {
        navigateFallback: '/index.html',
        // config.js is generated at container startup, so it must NOT be
        // precached (its build-time bytes are a dev placeholder).
        globIgnores: ['**/config.js'],
        runtimeCaching: [
          {
            urlPattern: ({ url }) => url.pathname === '/config.js',
            handler: 'StaleWhileRevalidate',
            options: { cacheName: 'runtime-config' },
          },
          {
            urlPattern: ({ url }) => /\/(chargers|sessions|stats)\b/.test(url.pathname),
            handler: 'NetworkFirst',
            options: { cacheName: 'api', networkTimeoutSeconds: 5, expiration: { maxAgeSeconds: 300, maxEntries: 200 } },
          },
          {
            urlPattern: ({ url }) => url.host.includes('tile.openstreetmap.org'),
            handler: 'CacheFirst',
            options: { cacheName: 'osm-tiles', expiration: { maxEntries: 600, maxAgeSeconds: 60 * 60 * 24 * 7 } },
          },
        ],
      },
    }),
  ],
})
