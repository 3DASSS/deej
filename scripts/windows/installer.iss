#define AppName "deej"
#define AppPublisher "das3d"
#define AppExeName "deej.exe"

#ifndef AppVersion
  #define AppVersion "v1.0.0"
#endif

; deej binaries to package; CI overrides Amd64Exe and defines Arm64Exe
; to produce a universal installer bundling both architectures
#ifndef Amd64Exe
  #define Amd64Exe "../../build/deej-release.exe"
#endif

[Setup]
AppId={{7CF11E9F-7191-458F-BE04-7520B911C391}
AppName={#AppName}
AppVerName={#AppName}
AppVersion={#AppVersion}
DefaultDirName={localappdata}\{#AppName}
OutputBaseFilename={#AppName}_setup
Compression=lzma
SolidCompression=yes
PrivilegesRequired=lowest
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#AppExeName}
SetupIconFile="..\..\pkg\icon\assets\logo.ico"
; "ArchitecturesAllowed=x64compatible" specifies that Setup cannot run
; on anything but x64 and Windows 11 on Arm.
ArchitecturesAllowed=x64compatible
; "ArchitecturesInstallIn64BitMode=x64compatible" requests that the
; install be done in "64-bit mode" on x64 or Windows 11 on Arm,
; meaning it should use the native 64-bit Program Files directory and
; the 64-bit view of the registry.
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes
WizardStyle=modern
WizardSizePercent=100
WizardSmallImageFile="..\..\pkg\icon\assets\logo.bmp"

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[CustomMessages]
english.EditConfig=Edit config file
russian.EditConfig=Редактировать конфигурацию
english.WV2Prompt=deej needs the Microsoft Edge WebView2 runtime for its settings window, but it was not found on this PC. Open the download page now?
russian.WV2Prompt=Для окна настроек deej требуется среда выполнения Microsoft Edge WebView2, но она не найдена на этом компьютере. Открыть страницу загрузки?

[Tasks]
Name: "autostart"; Description: "{cm:AutoStartProgram,{#AppName}}"

[Files]
#ifdef Arm64Exe
; install the binary matching the machine architecture
Source: "{#Amd64Exe}"; DestDir: "{app}"; DestName: {#AppExeName}; Check: not IsArm64; Flags: ignoreversion
Source: "{#Arm64Exe}"; DestDir: "{app}"; DestName: {#AppExeName}; Check: IsArm64; Flags: ignoreversion
#else
Source: "{#Amd64Exe}"; DestDir: "{app}"; DestName: {#AppExeName}; Flags: ignoreversion
#endif
Source: "../../config_examples/config.example.yaml"; DestDir: "{app}"; DestName: "config.yaml"; Flags: ignoreversion onlyifdoesntexist

[Registry]
; autostart
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "{#AppName}"; ValueData: "{app}\{#AppExeName}"; Tasks: autostart; Flags: uninsdeletevalue

[Run]
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchProgram,{#AppName}}"; Flags: postinstall nowait skipifsilent
Filename: {sys}\rundll32.exe; Parameters: "url.dll,FileProtocolHandler {app}\config.yaml"; Description: {cm:EditConfig}; Flags: postinstall nowait skipifsilent

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExeName}"

[UninstallDelete]
; delete logs
Type: filesandordirs; Name: "{app}/logs"

[UninstallRun]
; kill deej on uninstall
Filename: {sys}\taskkill.exe; Parameters: "/f /im {#AppExeName}"; Flags: skipifdoesntexist runhidden; RunOnceId: "KillProc"

[Code]
// deej's settings window is rendered by Edge WebView2. Recent Windows 10/11 ship
// the runtime in-box, but older/LTSC/Server images may not. We don't bundle the
// runtime; instead we detect it and, if missing, offer to open the download page.
// The runtime registers under a fixed GUID: system-wide installs land under
// HKLM\...\WOW6432Node (even on 64-bit), per-user installs under HKCU.
function WebView2Missing(): Boolean;
var
  Version: string;
begin
  Result := not (
    RegQueryStringValue(HKLM, 'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', Version)
    or RegQueryStringValue(HKCU, 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', Version));
  // treat an empty or 0.0.0.0 version as "not installed"
  if not Result and ((Version = '') or (Version = '0.0.0.0')) then
    Result := True;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if (CurStep = ssPostInstall) and WebView2Missing() then
  begin
    if MsgBox(ExpandConstant('{cm:WV2Prompt}'), mbConfirmation, MB_YESNO) = IDYES then
      ShellExec('open', 'https://developer.microsoft.com/microsoft-edge/webview2/', '', '', SW_SHOW, ewNoWait, ResultCode);
  end;
end;
