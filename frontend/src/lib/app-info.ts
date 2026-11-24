// app-info.ts holds the static facts shown in the settings about section. the
// version is only the fallback until the about screen reads the build's own.
export const APP = {
  name: 'Pelton',
  version: '0.1.0',
  website: 'https://pelton.app',
  docs: 'https://docs.pelton.app',
  repo: 'https://github.com/peltonapp/Pelton',
  issues: 'https://github.com/peltonapp/Pelton/issues',
  contributors: 'https://github.com/peltonapp/Pelton/graphs/contributors',
  license: 'GPL-3.0',
  licenseUrl: 'https://www.gnu.org/licenses/gpl-3.0.html',
  author: 'Arne K.',
} as const
