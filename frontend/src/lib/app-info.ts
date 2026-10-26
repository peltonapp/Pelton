// app-info.ts holds the static facts shown in the settings about section. the
// version is hardcoded for now; wiring it to the build (ldflags or wails.json)
// is a small follow-up.
// TODO(build): inject version at build time instead of hardcoding here.
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
