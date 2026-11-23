# Install tests (Windows)

Run on the development PC (Windows 11 Pro, French), **not** on a clean machine:
Windows Sandbox is not enabled here (enabling it needs administrator rights and
a reboot), so a true clean-machine test is still to do.

What was checked, with the 0.5.0 packages:

- `objdump -p vaultic.exe`: the executable imports only Windows system DLLs
  (kernel32, user32, gdi32, shell32, opengl32, the Universal C Runtime
  `api-ms-win-crt-*`). No Visual C++ redistributable is needed. OpenGL is
  needed by the interface; on a machine without a graphics driver Windows uses
  its software renderer, which has not been tried here.
- Setup, silent, into `...\Programmes Тест\Vaultic` (spaces and Cyrillic):
  exit 0; 8 files; first run writes its log under the user profile
  (`%APPDATA%\Vaultic\logs`, here a redirected profile); a second launch exits
  and leaves one process; silent uninstall exit 0 and leaves no file.
- Portable zip extracted into `...\Мой портатив test\`: runs, keeps its log in
  `logs\` next to the executable, writes nothing to the user profile except
  the font cache of the GUI toolkit.
- Fixed: the zips did not contain `LICENSE.fyne` (the setup did); `package.py`
  now adds it.

Not checked: a machine without the French locale, the setup wizard shown
(only `/VERYSILENT`), SmartScreen / antivirus reactions to an unsigned
executable, upgrade over an older version, Windows 10, ARM.

Known cosmetic output: at start, the tray library may print
`systray error: unable to set icon: tray not ready yet` on stderr.
