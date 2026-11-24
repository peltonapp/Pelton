// controls.ts maps the control an input kind names to the component that
// asks it. A question kind registered later in the backend that names one of
// these controls works without a change; a new control registers here. Each
// control gets the question as `ask` and binds `value`; it may dispatch
// `answer` to submit at once, e.g. on a double click.

import type { ComponentType } from 'svelte'
import TextControl from './TextControl.svelte'
import ChoiceControl from './ChoiceControl.svelte'
import FolderControl from './FolderControl.svelte'
import AccountControl from './AccountControl.svelte'
import ContactControl from './ContactControl.svelte'
import DateTimeControl from './DateTimeControl.svelte'

/** How a control asks. yesNo replaces the OK button with No and Yes, which answer false and true. */
export interface AskControl {
  component: ComponentType | null
  yesNo?: boolean
  /** The value the control starts from when the question has no default. */
  empty?: unknown
}

const controls = new Map<string, AskControl>()

/** Adds the control a question kind can name. */
export function registerAskControl(name: string, control: AskControl): void {
  controls.set(name, control)
}

/** The control of a name, or the text control for one nobody registered. */
export function askControl(name: string): AskControl {
  return controls.get(name) ?? controls.get('text')!
}

registerAskControl('text', { component: TextControl as unknown as ComponentType, empty: '' })
registerAskControl('textarea', { component: TextControl as unknown as ComponentType, empty: '' })
registerAskControl('choice', { component: ChoiceControl as unknown as ComponentType })
registerAskControl('confirm', { component: null, yesNo: true })
registerAskControl('folder', { component: FolderControl as unknown as ComponentType, empty: '' })
registerAskControl('account', { component: AccountControl as unknown as ComponentType })
registerAskControl('contact', { component: ContactControl as unknown as ComponentType })
registerAskControl('datetime', { component: DateTimeControl as unknown as ComponentType })
