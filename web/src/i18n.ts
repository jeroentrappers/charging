import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import en from './locales/en.json'
import nl from './locales/nl.json'
import fr from './locales/fr.json'

// English is the source language; Dutch and French are translations.
// Resource files live in src/locales/*.json (i18next JSON — a Weblate format).
export const LANGS = [
  { code: 'en', label: 'English' },
  { code: 'nl', label: 'Nederlands' },
  { code: 'fr', label: 'Français' },
] as const

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      nl: { translation: nl },
      fr: { translation: fr },
    },
    fallbackLng: 'en',
    supportedLngs: ['en', 'nl', 'fr'],
    nonExplicitSupportedLngs: true, // nl-BE -> nl, fr-BE -> fr
    interpolation: { escapeValue: false },
    detection: { order: ['localStorage', 'navigator'], caches: ['localStorage'] },
  })

export default i18n
