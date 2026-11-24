// the tesseract languages bundled with the app, by ui locale: one for every
// language Pelton itself is translated into. vite.config.ts copies their data
// files into the build, and the pdf viewer reads with the one matching the ui
// unless the settings pick others. more can be added from disk in the settings.
export const bundledOCRLanguages: Record<string, string> = {
  en: 'eng',
  de: 'deu',
  es: 'spa',
  fr: 'fra',
  it: 'ita',
  nl: 'nld',
  pl: 'pol',
  pt: 'por',
  ru: 'rus',
  tr: 'tur',
  ar: 'ara',
  'zh-CN': 'chi_sim',
}

export const bundledOCRCodes: string[] = Object.values(bundledOCRLanguages)
