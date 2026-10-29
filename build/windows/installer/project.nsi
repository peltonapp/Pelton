Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## MultiUser (per-machine vs per-user install choice) needs "Highest" instead
## of the default "admin" so the installer can run un-elevated and offer to
## elevate only if the user picks the all-users option. Must be set before
## wails_tools.nsh is included, since it only defines REQUEST_EXECUTION_LEVEL
## if nothing has already.
####
!define REQUEST_EXECUTION_LEVEL "Highest"
####
## Build channel. The nightly workflow writes a channel.nsh next to this file
## containing `!define PELTON_NIGHTLY`; a normal release build has no such file
## and the /NONFATAL include is a no-op. Everything guarded by PELTON_NIGHTLY
## below is therefore absent from release installers entirely.
####
!include /NONFATAL "channel.nsh"
####
## The AppUserModelID Pelton posts notifications under. Windows reads the name
## and icon of a toast from this key, so the app registers it at startup (see
## internal/desktop/notify_windows.go) rather than leaving it to the installer,
## which would strand portable builds and every copy installed before that
## change. The uninstaller removes it. Must match notifyAppName on the Go side.
####
!ifdef PELTON_NIGHTLY
    !define PELTON_AUMID "Pelton Nightly"
!else
    !define PELTON_AUMID "Pelton"
!endif
####
## Include the wails tools
####
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

####
## Per-machine (all users, needs admin) vs per-user (no admin) install choice.
## Defaults preserve the old behavior (all users) unless the user picks the
## other option on the new page this adds.
####
!define MULTIUSER_EXECUTIONLEVEL Highest
!define MULTIUSER_MUI
!define MULTIUSER_INSTALLMODE_COMMANDLINE
!define MULTIUSER_INSTALLMODE_DEFAULT_REGISTRY_KEY "Software\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!define MULTIUSER_INSTALLMODE_INSTDIR "${INFO_PRODUCTNAME}"
!define MULTIUSER_USE_PROGRAMFILES64
!include "MultiUser.nsh"

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
!insertmacro MUI_PAGE_LICENSE "..\..\..\LICENSE" # GPL-3.0. Kept in the original English text on every install
                                                  # language: unofficial translations of the GPL aren't legally
                                                  # authoritative, so the FSF recommends distributing only this text.

####
## Warranty and liability, an additional term under GPLv3 section 7(a). It is a
## separate page because the GPL text has to stay verbatim. disclaimer.txt is
## generated from DISCLAIMER.md by `make disclaimer` and carries both the
## English and the authoritative German wording, so it stays readable on every
## installer language.
####
!define MUI_PAGE_HEADER_TEXT "Warranty and liability"
!define MUI_PAGE_HEADER_SUBTEXT "Please read the terms under which Pelton is provided."
!define MUI_LICENSEPAGE_TEXT_TOP "Pelton is provided free of charge and without warranty."
!define MUI_LICENSEPAGE_TEXT_BOTTOM "Click Agree to continue installing Pelton."
!define MUI_LICENSEPAGE_BUTTON "Agree"
!insertmacro MUI_PAGE_LICENSE "disclaimer.txt"

####
## Nightly builds get one more page, after the terms and before anything is
## chosen or written, spelling out that this is an untested build that must not
## be pointed at a real inbox. The "I understand" button is the only way past
## it. Release installers never see this block.
####
!ifdef PELTON_NIGHTLY
    !define MUI_PAGE_HEADER_TEXT "Nightly build"
    !define MUI_PAGE_HEADER_SUBTEXT "This is not a release. Please read before installing."
    !define MUI_LICENSEPAGE_TEXT_TOP "You are about to install an untested nightly build."
    !define MUI_LICENSEPAGE_TEXT_BOTTOM "Click I understand to install this nightly build at your own risk."
    !define MUI_LICENSEPAGE_BUTTON "I understand"
    !insertmacro MUI_PAGE_LICENSE "nightly.txt"
!endif

!insertmacro MULTIUSER_PAGE_INSTALLMODE # All users (admin) vs just me (no admin).
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_COMPONENTS # Which shortcuts to create.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}" # Offer to launch Pelton right after install, checked by default.
!define MUI_FINISHPAGE_TEXT "${INFO_PRODUCTNAME} has been installed.$\r$\n$\r$\nTo pin it to the taskbar, start ${INFO_PRODUCTNAME}, then right-click its taskbar button and choose $\"Pin to taskbar$\". Windows does not allow an installer to do this for you."
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

# Installer languages, kept in lockstep with Pelton's own UI languages
# (frontend/src/lib/locales). English first so it's the dialog's default. This
# only controls the installer's own UI language; Pelton itself always ships
# with every language embedded (they're a few KB each), so there's nothing to
# select or exclude there.
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "French"
!insertmacro MUI_LANGUAGE "Dutch"
!insertmacro MUI_LANGUAGE "Spanish"

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}" # Fallback default; MULTIUSER_INIT overrides this based on the page above.
ShowInstDetails show # This will always show the installation details.

# wails.setShellContext (from wails_tools.nsh) picks the shell context from the
# REQUEST_EXECUTION_LEVEL *string define*, which is now "Highest" to support
# both install modes - so that macro would always fall through to the
# per-user branch. This reads the actual runtime choice from MultiUser instead.
!macro pelton.setShellContext
    ${If} $MultiUser.InstallMode == "AllUsers"
        SetShellVarContext all
    ${Else}
        SetShellVarContext current
    ${EndIf}
!macroend

!macro pelton.stopRunningApp
    nsExec::ExecToLog 'taskkill /IM "${PRODUCT_EXECUTABLE}" /F'
    Pop $0
    Sleep 500
!macroend

Function .onInit
!ifdef PELTON_NIGHTLY
   ; before the installer ui is even up, so nobody double-clicks their way past
   ; the page below without registering what they downloaded.
   MessageBox MB_YESNO|MB_ICONEXCLAMATION|MB_DEFBUTTON2 "This is a NIGHTLY build of ${INFO_PRODUCTNAME}, not a release.$\r$\n$\r$\nIt is untested, expected to be broken, and can lose or damage email. Do not use it with your real inbox.$\r$\n$\r$\nContinue installing?" IDYES pelton_nightly_ok
       Quit
   pelton_nightly_ok:
!endif
   !insertmacro MULTIUSER_INIT # reads/handles the all-users vs per-user choice first; may relaunch elevated.
   ${If} $MultiUser.InstallMode == "AllUsers"
       UserInfo::GetAccountType
       Pop $0
       ${If} $0 != "Admin"
           MessageBox MB_OK|MB_ICONEXCLAMATION "Installing ${INFO_PRODUCTNAME} for all users requires administrator rights.$\r$\n$\r$\nRe-run this installer as an administrator, or choose $\"Install just for me$\" instead."
           Quit
       ${EndIf}
   ${EndIf}
   !insertmacro MUI_LANGDLL_DISPLAY # ask which of the installer languages above to use
   !insertmacro wails.checkArchitecture
FunctionEnd

Function un.onInit
   !insertmacro MULTIUSER_UNINIT # recovers the install mode this copy was installed with.
   !insertmacro MUI_UNGETLANGUAGE
   MessageBox MB_YESNO|MB_ICONQUESTION "Are you sure you want to completely remove ${INFO_PRODUCTNAME} and all of its components?" IDYES un_confirmed
       Quit
   un_confirmed:
FunctionEnd

!define PELTON_REG_KEY "Software\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"

; wails.writeUninstaller always writes the Apps and Features entry to HKLM,
; which a per-user install has no rights to, so that install never showed up
; there. These write it to SHCTX, which follows the install mode.
!macro pelton.writeUninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    SetRegView 64
    WriteRegStr SHCTX "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr SHCTX "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr SHCTX "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr SHCTX "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr SHCTX "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr SHCTX "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD SHCTX "${UNINST_KEY}" "EstimatedSize" "$0"
!macroend

!macro pelton.deleteUninstaller
    Delete "$INSTDIR\uninstall.exe"

    SetRegView 64
    DeleteRegKey SHCTX "${UNINST_KEY}"
!macroend

Section "-Core" SecCore
    ; leading "-" hides this from the components list and keeps it mandatory:
    ; the app itself and its file/protocol associations always install
    ; regardless of what's checked below. The shortcuts are separate sections.
    !insertmacro pelton.setShellContext

    !insertmacro pelton.stopRunningApp

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    File "..\..\..\LICENSE"
    File "..\..\..\README.md"
    SetOutPath "$INSTDIR\licenses"
    File /r "..\..\..\licenses\*.*"
    SetOutPath $INSTDIR

    WriteRegStr SHCTX "${PELTON_REG_KEY}" "StartMenuShortcut" "0"
    WriteRegStr SHCTX "${PELTON_REG_KEY}" "DesktopShortcut" "0"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro pelton.writeUninstaller
SectionEnd

Section "Start Menu Shortcut" SecStartMenuShortcut
    !insertmacro pelton.setShellContext
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr SHCTX "${PELTON_REG_KEY}" "StartMenuShortcut" "1"
SectionEnd

Section "Desktop Shortcut" SecDesktopShortcut
    !insertmacro pelton.setShellContext
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr SHCTX "${PELTON_REG_KEY}" "DesktopShortcut" "1"
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${SecStartMenuShortcut} "Add ${INFO_PRODUCTNAME} to the Start Menu."
    !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktopShortcut} "Put a ${INFO_PRODUCTNAME} shortcut on the desktop."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Section "uninstall"
    !insertmacro pelton.setShellContext

    !insertmacro pelton.stopRunningApp

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\LICENSE"
    Delete "$INSTDIR\README.md"
    RMDir /r "$INSTDIR\licenses"
    RMDir /r $INSTDIR

    ; an empty value means a copy installed before the shortcuts were recorded,
    ; where both were the installer's to remove.
    ReadRegStr $0 SHCTX "${PELTON_REG_KEY}" "StartMenuShortcut"
    ReadRegStr $1 SHCTX "${PELTON_REG_KEY}" "DesktopShortcut"
    ${If} $0 != "0"
        Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    ${EndIf}
    ${If} $1 != "0"
        Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    ${EndIf}
    DeleteRegValue SHCTX "${PELTON_REG_KEY}" "StartMenuShortcut"
    DeleteRegValue SHCTX "${PELTON_REG_KEY}" "DesktopShortcut"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    ; the notification registration Pelton writes at startup. Always HKCU, so
    ; this only clears it for the account running the uninstaller; an all-users
    ; install leaves the other accounts' keys behind, which are three inert
    ; strings pointing at an executable that is gone.
    DeleteRegKey HKCU "Software\Classes\AppUserModelId\${PELTON_AUMID}"

    !insertmacro pelton.deleteUninstaller
SectionEnd
