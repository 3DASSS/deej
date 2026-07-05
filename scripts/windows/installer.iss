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
