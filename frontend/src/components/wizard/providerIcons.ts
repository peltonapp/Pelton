// providerIcons.ts maps a provider preset to the Phosphor glyph the wizard
// draws for it, falling back to a plain envelope.
import google from '@phosphor-icons/core/assets/duotone/google-logo-duotone.svg?raw'
import outlook from '@phosphor-icons/core/assets/duotone/microsoft-outlook-logo-duotone.svg?raw'
import apple from '@phosphor-icons/core/assets/duotone/apple-logo-duotone.svg?raw'
import envelope from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
import server from '@phosphor-icons/core/assets/duotone/hard-drives-duotone.svg?raw'

const icons: Record<string, string> = {
  gmail: google,
  outlook,
  icloud: apple,
  custom: server,
}

/** providerIcon returns the raw svg for a preset id. */
export function providerIcon(id: string): string {
  return icons[id] ?? envelope
}
