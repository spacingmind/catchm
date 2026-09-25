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
## Include the wails tools
####
!include "wails_tools.nsh"

# M22 §5: the AUMID toast notifications need on the Start-menu shortcut
# (SetCurrentProcessExplicitAppUserModelID at startup — cmd/catchm-
# desktop/aumid_windows.go — must use this exact same string; also
# SingleInstanceLock's UniqueId, main.go's appID const).
!define APP_AUMID "com.catchm.desktop"

# The launch-at-login Run-key value name (cmd/catchm-desktop/
# autostart_windows.go's autostartValueName) — removed on uninstall
# below so a stale entry can't outlive the app.
!define AUTOSTART_VALUE_NAME "CatchM"

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

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
  !else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
  !endif
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    # M22 §5: the Start-menu shortcut needs its System.AppUserModel.ID
    # property stamped for toast notifications to attribute to CatchM
    # instead of a generic/PowerShell identity (required in addition to
    # SetCurrentProcessExplicitAppUserModelID at process startup, not
    # instead of it — both must agree, see aumid_windows.go). NSIS's own
    # CreateShortcut has no AUMID switch, and the standard third-party
    # plugin for it (WinShell) isn't installed on this build machine's
    # NSIS (confirmed live: the installer aborted with "Plugin not
    # found, cannot call WinShell::SetLnkAUMI" the first time this was
    # tried). Even where WinShell IS available, live testing found
    # stamping a shortcut AFTER a separate tool (CreateShortcut) already
    # created it is unreliable — IPersistFile::Load on a shortcut this
    # process didn't create intermittently returns a degraded property
    # store (S_FALSE, near-empty) where every subsequent write call
    # still reports S_OK but silently doesn't persist. New-
    # AumidShortcut.ps1 (embedded via File below, run through the
    # PowerShell that ships with every supported Windows version — no
    # NSIS plugin needed) creates the Start-menu shortcut AND stamps the
    # AUMID in ONE COM session instead, which persists reliably (tested
    # 5/5 against a fresh process re-open before relying on it here).
    File "/oname=$PLUGINSDIR\New-AumidShortcut.ps1" "New-AumidShortcut.ps1"
    nsExec::ExecToLog '"$WINDIR\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\New-AumidShortcut.ps1" -ShortcutPath "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" -TargetPath "$INSTDIR\${PRODUCT_EXECUTABLE}" -WorkingDirectory "$INSTDIR" -IconPath "$INSTDIR\${PRODUCT_EXECUTABLE}" -AppUserModelID "${APP_AUMID}"'
    Pop $0
    DetailPrint "New-AumidShortcut.ps1 exit code: $0"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    # M22 §5: launch-at-login is opt-in (HKCU Run key, written by
    # autostart_windows.go only when the user turns the Settings toggle
    # on) — the uninstaller removes it unconditionally so it can't
    # outlive the app; deleting an absent value is a silent no-op in
    # NSIS, so this is safe whether or not the user ever enabled it.
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${AUTOSTART_VALUE_NAME}"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
