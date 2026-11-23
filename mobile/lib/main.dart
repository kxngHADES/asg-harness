import 'package:material_ui/material_ui.dart';

import 'models/asg_models.dart';
import 'screens/connect_screen.dart';
import 'screens/settings_screen.dart';
import 'screens/workspace_screen.dart';
import 'services/host_storage.dart';
import 'theme/asg_theme.dart';

void main() {
  runApp(const AsgApp());
}

class AsgApp extends StatefulWidget {
  const AsgApp({super.key});

  @override
  State<AsgApp> createState() => _AsgAppState();
}

class _AsgAppState extends State<AsgApp> {
  HostInfo? _connectedHost;
  ThemeMode _themeMode = ThemeMode.system;

  @override
  void initState() {
    super.initState();
    HostStorage.loadThemeMode().then((mode) {
      if (mounted) setState(() => _themeMode = mode);
    });
  }

  void _setThemeMode(ThemeMode mode) {
    setState(() => _themeMode = mode);
    HostStorage.saveThemeMode(mode);
  }

  void _openDisconnectedSettings(BuildContext ctx) {
    Navigator.of(ctx).push(
      MaterialPageRoute<void>(
        builder: (_) => SettingsScreen(
          themeMode: _themeMode,
          onThemeModeChanged: _setThemeMode,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'ASG',
      debugShowCheckedModeBanner: false,
      themeMode: _themeMode,
      theme: buildAsgTheme(Brightness.light),
      darkTheme: buildAsgTheme(Brightness.dark),
      home: _connectedHost == null
          ? Builder(
              builder: (ctx) => ConnectScreen(
                onConnected: (host) => setState(() => _connectedHost = host),
                onOpenSettings: () => _openDisconnectedSettings(ctx),
              ),
            )
          : WorkspaceScreen(
              key: ValueKey(_connectedHost!.baseUrl),
              host: _connectedHost!,
              themeMode: _themeMode,
              onThemeModeChanged: _setThemeMode,
              onDisconnect: () => setState(() => _connectedHost = null),
            ),
    );
  }
}
