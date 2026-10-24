import Foundation
import ServiceManagement

/// Open-at-login via `SMAppService.mainApp` (macOS 13+ Login Item).
enum LoginItem {
    private static let prefKey = "launchAtLoginDesired"

    /// Whether the OS currently has Klient registered as a Login Item.
    static var isEnabled: Bool {
        SMAppService.mainApp.status == .enabled
    }

    static var status: SMAppService.Status {
        SMAppService.mainApp.status
    }

    static var requiresApproval: Bool {
        SMAppService.mainApp.status == .requiresApproval
    }

    /// UserDefaults mirror of the last chosen value (SMAppService is source of truth for UI).
    static var desired: Bool {
        get { UserDefaults.standard.bool(forKey: prefKey) }
        set { UserDefaults.standard.set(newValue, forKey: prefKey) }
    }

    /// Registers or unregisters the main app as a Login Item.
    static func setEnabled(_ enabled: Bool) throws {
        let svc = SMAppService.mainApp
        if enabled {
            if svc.status == .enabled {
                desired = true
                return
            }
            try svc.register()
        } else {
            if svc.status == .notRegistered {
                desired = false
                return
            }
            try svc.unregister()
        }
        desired = enabled
    }

    /// Opens System Settings → General → Login Items when approval is needed.
    static func openSystemSettingsLoginItems() {
        SMAppService.openSystemSettingsLoginItems()
    }
}
