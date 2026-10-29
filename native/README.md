# cmnet Native Desktop (Windows, Preview)

This is a **true WPF Windows UI**: Windows Presentation Foundation renders controls
through Windows graphics, with no HTML renderer, Chromium, Tauri or WebView2.

It reuses the existing Go `advisor.exe` backend, local API and SSE stream.
The WPF executable and `advisor.exe` must live next to each other; the native
frontend spawns the core on a random loopback port and terminates it on exit.
User data remains in the **same** cmnet folder used by Electron (`desktop.json`
`dataDir` is respected). Database and models are not migrated or duplicated.

## Build / run

On Windows with the .NET 8 SDK and Go 1.26:

```powershell
$env:CGO_ENABLED = '0'
go build -o native/out/advisor.exe ./cmd/advisor
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out -p:PublishSingleFile=true
./native/out/Cmnet.Native.exe
```

Or download the `cmnet-native-preview-win-x64` artifact from the
`native-windows` GitHub Actions run and unzip it before launching.

## Implemented in this preview

- Native window, navigation, WPF list virtualization, Windows high-DPI scaling
- Go process lifecycle / safe shutdown and SSE reconnect
- Scout roster (five allies + five enemies), cached Steam personas / local avatars,
  address book, seat changes, ally/enemy scouting and full-roster refresh
- Draft status / hero selection / recommendations / undo / redo
- Data jobs (sync/train), basic model / corpus status
- Read-only config display and directory opener
- Basic host sharing / receiving by code

## Not yet parity-complete (issue #17 remains OPEN)

The existing Electron client remains the **production version**. Advanced Settings
editing (alerts/Discord/rules), drag/drop roster placement, chart/corpus explorer,
GSI feedback ergonomics, full sharing flows, signed NSIS/MSIX installer,
native auto-update, cross-version migration, multi-monitor QA and 60-fps
interactive acceptance remain to be implemented/tested. No default desktop
installer is replaced by this preview. Avoid running Electron and native cmnet
against the same data directory at the same time.
