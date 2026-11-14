; TaskLens Windows installer. Built by packaging/build.sh with makensis.
Unicode true
!include "MUI2.nsh"
!include "x64.nsh"

Name "TaskLens"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\TaskLens"
InstallDirRegKey HKLM "Software\TaskLens" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "TaskLens"
VIAddVersionKey "FileDescription" "TaskLens installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "TaskLens"

!define MUI_ICON "tasklens.ico"
!define MUI_UNICON "tasklens.ico"
!define MUI_FINISHPAGE_RUN "$INSTDIR\TaskLens.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open TaskLens now"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

!define UNINST "Software\Microsoft\Windows\CurrentVersion\Uninstall\TaskLens"

Section "Install"
  SetRegView 64
  SetOutPath "$INSTDIR"
  File "/oname=TaskLens.exe" "${EXE}"
  File "tasklens.ico"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortcut "$SMPROGRAMS\TaskLens.lnk" "$INSTDIR\TaskLens.exe" "" "$INSTDIR\tasklens.ico"
  CreateShortcut "$DESKTOP\TaskLens.lnk" "$INSTDIR\TaskLens.exe" "" "$INSTDIR\tasklens.ico"
  WriteRegStr HKLM "Software\TaskLens" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${UNINST}" "DisplayName" "TaskLens"
  WriteRegStr HKLM "${UNINST}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST}" "Publisher" "TaskLens"
  WriteRegStr HKLM "${UNINST}" "DisplayIcon" "$INSTDIR\tasklens.ico"
  WriteRegStr HKLM "${UNINST}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKLM "${UNINST}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetRegView 64
  ExecWait 'taskkill /F /IM TaskLens.exe'
  Delete "$INSTDIR\TaskLens.exe"
  Delete "$INSTDIR\tasklens.ico"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\TaskLens.lnk"
  Delete "$DESKTOP\TaskLens.lnk"
  DeleteRegKey HKLM "${UNINST}"
  DeleteRegKey HKLM "Software\TaskLens"
SectionEnd
