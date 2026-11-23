import 'package:material_ui/material_ui.dart';
import 'package:shared_preferences/shared_preferences.dart';

class HostStorage {
  static const _keyLastHost = 'asg.last_host';
  static const _keyRecentHosts = 'asg.recent_hosts';
  static const _keyThemeMode = 'asg.theme_mode';

  static Future<ThemeMode> loadThemeMode() async {
    final prefs = await SharedPreferences.getInstance();
    switch (prefs.getString(_keyThemeMode)) {
      case 'light':
        return ThemeMode.light;
      case 'dark':
        return ThemeMode.dark;
      default:
        return ThemeMode.system;
    }
  }

  static Future<void> saveThemeMode(ThemeMode mode) async {
    final prefs = await SharedPreferences.getInstance();
    final v = switch (mode) {
      ThemeMode.light => 'light',
      ThemeMode.dark => 'dark',
      ThemeMode.system => 'system',
    };
    await prefs.setString(_keyThemeMode, v);
  }

  static Future<String?> loadLastHost() async {
    final prefs = await SharedPreferences.getInstance();
    final v = prefs.getString(_keyLastHost);
    if (v == null || v.trim().isEmpty) return null;
    return v.trim();
  }

  static Future<List<String>> loadRecentHosts() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getStringList(_keyRecentHosts) ?? const [];
  }

  static Future<void> saveHost(String address) async {
    final clean = address.trim();
    if (clean.isEmpty) return;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyLastHost, clean);
    final recent = List<String>.from(prefs.getStringList(_keyRecentHosts) ?? []);
    recent.remove(clean);
    recent.insert(0, clean);
    if (recent.length > 6) {
      recent.removeRange(6, recent.length);
    }
    await prefs.setStringList(_keyRecentHosts, recent);
  }

  static Future<void> removeRecent(String address) async {
    final prefs = await SharedPreferences.getInstance();
    final recent = List<String>.from(prefs.getStringList(_keyRecentHosts) ?? []);
    recent.remove(address);
    await prefs.setStringList(_keyRecentHosts, recent);
  }

  static Future<void> clearLastHost() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_keyLastHost);
  }
}
