import 'package:flutter/material.dart';

void main() {
  runApp(const AsgApp());
}

class AsgApp extends StatelessWidget {
  const AsgApp({super.key});

  static const _night = Color(0xFF0D100E);
  static const _clay = Color(0xFFE1E6DE);
  static const _lime = Color(0xFFC6F432);

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'ASG',
      debugShowCheckedModeBanner: false,
      themeMode: ThemeMode.system,
      theme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: _lime,
          brightness: Brightness.light,
          surface: _clay,
        ),
      ),
      darkTheme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: _lime,
          brightness: Brightness.dark,
          surface: _night,
        ),
      ),
      home: const _HomePlaceholder(),
    );
  }
}

class _HomePlaceholder extends StatelessWidget {
  const _HomePlaceholder();

  @override
  Widget build(BuildContext context) {
    final textTheme = Theme.of(context).textTheme;
    return Scaffold(
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(24),
                child: Image.asset(
                  'assets/icon/appicon.png',
                  width: 96,
                  height: 96,
                  filterQuality: FilterQuality.none,
                ),
              ),
              const SizedBox(height: 16),
              Text('ASG', style: textTheme.headlineMedium),
              const SizedBox(height: 6),
              Text(
                'Mobile client ready for development',
                style: textTheme.bodyMedium,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
