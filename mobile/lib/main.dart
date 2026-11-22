import 'package:material_ui/material_ui.dart';

import 'models/asg_models.dart';
import 'screens/connect_screen.dart';
import 'screens/workspace_screen.dart';
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

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'ASG',
      debugShowCheckedModeBanner: false,
      themeMode: ThemeMode.system,
      theme: buildAsgTheme(Brightness.light),
      darkTheme: buildAsgTheme(Brightness.dark),
      home: _connectedHost == null
          ? ConnectScreen(
              onConnected: (host) => setState(() => _connectedHost = host),
            )
          : WorkspaceScreen(
              key: ValueKey(_connectedHost!.baseUrl),
              host: _connectedHost!,
              onDisconnect: () => setState(() => _connectedHost = null),
            ),
    );
  }
}
