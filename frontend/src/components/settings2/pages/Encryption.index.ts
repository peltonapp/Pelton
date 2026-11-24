import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  {
    page: 'encryption',
    id: 'auto-encrypt',
    labelKey: 'autoEncrypt.settings.switch',
    keywords: 'automatic encryption encrypt autocrypt wkd web key directory keys.openpgp.org key server lookup subject backup setup message another device fingerprint verify compare',
  },
  {
    page: 'encryption',
    id: 'keys',
    labelKey: 'settingsPanel.category.encryption',
    keywords: 'pgp gpg openpgp key keys import export delete unlock passphrase signing sign account search encrypted decrypted index smime s/mime revocation ocsp crl certificate withdrawn',
  },
]

export default entries
