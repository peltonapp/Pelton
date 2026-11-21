// cliapi.ts is the command line access part of the Go api. Like
// mailvaultapi.ts it calls the bound methods by name, so none of it sits in
// the window's startup code. The backend test TestCLIAPINamesAreBound checks
// every name below against the App.

import { Call, Events } from '@wailsio/runtime'

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`github.com/peltonapp/Pelton/internal/desktop.App.${method}`, ...args) as Promise<T>
}

/** One capability switch of command line access. */
export interface CLIPermission {
  name: string
  allowed: boolean
}

/** Command line access as the settings show it. */
export interface CLIConfig {
  enabled: boolean
  running: boolean
  socket: string
  tokenFile: string
  permissions: CLIPermission[]
}

/** One request the pelton command made. */
export interface CLILogEntry {
  id: number
  command: string
  touched: string
  result: string
  switch: string
  when: string
}

/** A folder the pelton link can go in. */
export interface CLIInstallDir {
  dir: string
  installed: boolean
}

/** Whether and where the command line tool can be installed. */
export interface CLIInstallInfo {
  supported: boolean
  executable: string
  onPath: string
  dirs: CLIInstallDir[]
}

/** The link made, or the command to run for a folder that needs admin rights. */
export interface CLIInstallResult {
  path: string
  command: string
}

/** The command line access settings. */
export function getCLIConfig(): Promise<CLIConfig> {
  return call('GetCLIConfig')
}

/** Turns command line access on or off. */
export function setCLIEnabled(enabled: boolean): Promise<void> {
  return call('SetCLIEnabled', enabled)
}

/** Turns one capability on or off. */
export function setCLIPermission(name: string, allowed: boolean): Promise<void> {
  return call('SetCLIPermission', name, allowed)
}

/** The newest requests of the pelton command. */
export function cliLog(): Promise<CLILogEntry[]> {
  return call('CLILog')
}

/** Empties the request log. */
export function clearCLILog(): Promise<void> {
  return call('ClearCLILog')
}

/** Describes installing the command line tool here. */
export function cliInstallInfo(): Promise<CLIInstallInfo> {
  return call('CLIInstallInfo')
}

/** Asks for a folder to install into; '' when cancelled. */
export function chooseCLIInstallDir(): Promise<string> {
  return call('ChooseCLIInstallDir')
}

/** Puts the pelton link into dir. */
export function installCLI(dir: string): Promise<CLIInstallResult> {
  return call('InstallCLI', dir)
}

/** Fires when the request log changes. */
export function onCLILog(cb: () => void): () => void {
  return Events.On('cli:log', () => cb())
}
