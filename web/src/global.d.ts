export {}

declare global {
  interface Window {
    // Injected at runtime by /config.js (generated from env on container start).
    __CONFIG__?: { apiBase?: string }
  }
}
