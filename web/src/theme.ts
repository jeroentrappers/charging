import { useEffect, useState } from 'react'

// Theme preference, persisted in the browser. 'system' follows the OS setting
// live. The effective theme is reflected as data-theme on <html>, which drives
// the CSS palette (see styles.css). An inline script in index.html applies it
// before first paint to avoid a flash.
export type Theme = 'light' | 'dark' | 'system'

export const KEY = 'charging.theme'
const DARK_BAR = '#0b1220' // status-bar / theme-color in dark
const LIGHT_BAR = '#15803d'

function prefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

export function resolveTheme(t: Theme): 'light' | 'dark' {
  return t === 'system' ? (prefersDark() ? 'dark' : 'light') : t
}

function apply(t: Theme) {
  const eff = resolveTheme(t)
  document.documentElement.dataset.theme = eff
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', eff === 'dark' ? DARK_BAR : LIGHT_BAR)
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setTheme] = useState<Theme>(() => {
    try {
      return (localStorage.getItem(KEY) as Theme) || 'system'
    } catch {
      return 'system'
    }
  })

  useEffect(() => {
    apply(theme)
    try {
      localStorage.setItem(KEY, theme)
    } catch {
      /* ignore quota / private mode */
    }
    // While following the system, re-apply when the OS toggles dark/light.
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => apply('system')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme])

  return [theme, setTheme]
}
