package icon

import _ "embed"

// TrayDeejLogo is a binary representation of the deej logo; used for notifications and tray icon.
// PNG, because wails' systray icon loader accepts PNG data but not full .ico containers
//go:embed assets/tray-icon.png
var TrayDeejLogo []byte
