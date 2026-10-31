; CMNET native Windows installer (Gate 3.2)
; Builds a per-user NSIS package for Cmnet.Native.exe + advisor.exe sidecar.
; Distinct from Electron cmnet-setup-*.exe (desktop.yml / v0.3.x channel).
; UNSIGNED — signing / update channel = Gate 3.3. Do not claim Released.

!ifndef CMNET_VERSION
  !define CMNET_VERSION "0.4.0"
!endif
!ifndef CMNET_INFO_VERSION
  !define CMNET_INFO_VERSION "0.4.0-native"
!endif
!ifndef CMNET_PAYLOAD_DIR
  !define CMNET_PAYLOAD_DIR "..\out"
!endif
!ifndef CMNET_OUT_FILE
  !define CMNET_OUT_FILE "..\dist\cmnet-native-setup-${CMNET_VERSION}.exe"
!endif

Unicode true
CRCCheck on
SetCompressor /SOLID lzma
SetDatablockOptimize on

Name "CMNET"
BrandingText "CMNET ${CMNET_INFO_VERSION} (unsigned preview)"
OutFile "${CMNET_OUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\CMNET"
InstallDirRegKey HKCU "Software\CMNET\Native" "InstallDir"
RequestExecutionLevel user
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${CMNET_VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "CMNET"
VIAddVersionKey /LANG=1033 "FileDescription" "CMNET Match Intelligence (native)"
VIAddVersionKey /LANG=1033 "FileVersion" "${CMNET_INFO_VERSION}"
VIAddVersionKey /LANG=1033 "ProductVersion" "${CMNET_INFO_VERSION}"
VIAddVersionKey /LANG=1033 "CompanyName" "cmnet"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Local-first non-commercial"
VIAddVersionKey /LANG=1033 "Comments" "Unsigned Gate 3.2 preview — not Released"

!include "MUI2.nsh"
!include "FileFunc.nsh"

!define MUI_ABORTWARNING
!define MUI_ICON "..\Assets\cmnet.ico"
!define MUI_UNICON "..\Assets\cmnet.ico"
!define MUI_WELCOMEPAGE_TITLE "Install CMNET"
!define MUI_WELCOMEPAGE_TEXT "CMNET Match Intelligence (native WPF + Go core).$\r$\n$\r$\nThis build is UNSIGNED and is not a Released product. Auto-update arrives in Gate 3.3.$\r$\n$\r$\nNo Visual Studio, .NET SDK, or Go toolchain is required.$\r$\n$\r$\nClick Next to continue."
!define MUI_FINISHPAGE_RUN "$INSTDIR\Cmnet.Native.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open CMNET"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_LINK "User data stays in %APPDATA%\cmnet (not removed on uninstall)"
!define MUI_FINISHPAGE_LINK_LOCATION "https://github.com/twipz333/cmnet"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Section "CMNET" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"

  ; Self-contained WPF publish output + Go sidecar (must stay adjacent).
  File /r "${CMNET_PAYLOAD_DIR}\*.*"

  ; Start Menu + Desktop shortcuts (friend install path).
  CreateDirectory "$SMPROGRAMS\CMNET"
  CreateShortCut "$SMPROGRAMS\CMNET\CMNET.lnk" "$INSTDIR\Cmnet.Native.exe" "" "$INSTDIR\Cmnet.Native.exe" 0
  CreateShortCut "$DESKTOP\CMNET.lnk" "$INSTDIR\Cmnet.Native.exe" "" "$INSTDIR\Cmnet.Native.exe" 0

  WriteUninstaller "$INSTDIR\Uninstall.exe"

  WriteRegStr HKCU "Software\CMNET\Native" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "Software\CMNET\Native" "Version" "${CMNET_INFO_VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "DisplayName" "CMNET (native)"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "DisplayIcon" "$INSTDIR\Cmnet.Native.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "DisplayVersion" "${CMNET_INFO_VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "Publisher" "cmnet"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "QuietUninstallString" "$\"$INSTDIR\Uninstall.exe$\" /S"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "NoRepair" 1
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "Comments" "Unsigned preview — not Released; Electron channel untouched"

  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native" "EstimatedSize" "$0"
SectionEnd

Section "Uninstall"
  Delete "$DESKTOP\CMNET.lnk"
  Delete "$SMPROGRAMS\CMNET\CMNET.lnk"
  RMDir "$SMPROGRAMS\CMNET"

  ; Remove install tree only. NEVER delete %APPDATA%\cmnet user data
  ; (cmnet.db, desktop.json, diagnostics\, import-backup\, …). Gate 3.2 / 3.4.
  ; Do NOT add RMDir / Delete targeting $APPDATA\cmnet or $APPDATA\cmnet\*.
  RMDir /r "$INSTDIR"

  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native"
  DeleteRegKey HKCU "Software\CMNET\Native"
SectionEnd
