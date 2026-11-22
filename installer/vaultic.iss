; Inno Setup script for the Windows installer (https://jrsoftware.org/isinfo.php).
;
; Build:   py scripts/build.py   then   iscc installer\vaultic.iss
; Output:  dist\release\vaultic-<version>-windows-amd64-setup.exe
;
; Design: a per-user install (no administrator rights), program files only.
; Settings, history and passwords stay in the user's profile and in the
; Windows Credential Manager; uninstalling never deletes them.

#define AppName "Vaultic"
#define AppVersion Trim(FileRead(FileOpen("..\VAULTIC_VERSION")))

[Setup]
AppId={{B5E2C6C0-7A1E-4C57-9B0B-6F6C0B7E3A11}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=Stanislav SAVELIEV
AppPublisherURL=https://github.com/cybou-fr/vaultic
AppCopyright=Copyright 2026 Stanislav SAVELIEV
VersionInfoVersion={#AppVersion}
DefaultDirName={localappdata}\Programs\Vaultic
DefaultGroupName=Vaultic
PrivilegesRequired=lowest
OutputDir=..\dist\release
OutputBaseFilename=vaultic-{#AppVersion}-windows-amd64-setup
SetupIconFile=..\assets\vaultic.ico
UninstallDisplayIcon={app}\vaultic.exe
LicenseFile=..\LICENSE
Compression=lzma2
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
WizardStyle=modern
; Signing is optional: configure a SignTool in the Inno IDE and add
; "SignTool=mysigner" here when a code-signing certificate is available.

[Languages]
Name: "french"; MessagesFile: "compiler:Languages\French.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "..\dist\vaultic.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE.restic"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\NOTICE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\LICENSE.fyne"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Vaultic"; Filename: "{app}\vaultic.exe"
Name: "{autodesktop}\Vaultic"; Filename: "{app}\vaultic.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; Flags: unchecked

[Run]
Filename: "{app}\vaultic.exe"; Description: "{cm:LaunchProgram,Vaultic}"; Flags: nowait postinstall skipifsilent
