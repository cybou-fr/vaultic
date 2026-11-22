; Inno Setup script for the Windows installer (https://jrsoftware.org/isinfo.php).
;
; NOT BUILT OR TESTED in this repository: Inno Setup is not installed on the
; development machine. Build it by hand with "iscc installer\vaultic.iss"
; after "py scripts/build.py", then test the installer on a clean machine
; before publishing it.
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
AppPublisher=Vaultic
DefaultDirName={localappdata}\Programs\Vaultic
DefaultGroupName=Vaultic
PrivilegesRequired=lowest
OutputDir=..\dist\release
OutputBaseFilename=vaultic-{#AppVersion}-windows-amd64-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\vaultic.exe
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
