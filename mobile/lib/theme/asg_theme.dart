import 'package:material_ui/material_ui.dart';

/// Matches desktop/frontend/dist/app.css ("warm graphite shell, lime brand,
/// one colour per session kind").
@immutable
class AsgColors extends ThemeExtension<AsgColors> {
  const AsgColors({
    required this.bg,
    required this.side,
    required this.surface,
    required this.raised,
    required this.line,
    required this.text,
    required this.text2,
    required this.muted,
    required this.brand,
    required this.brandInk,
    required this.userBubble,
    required this.codeBg,
    required this.danger,
    required this.warn,
    required this.ok,
    required this.kindChat,
    required this.kindCode,
    required this.kindArtifact,
    required this.kindProject,
    required this.kindScheduled,
  });

  final Color bg;
  final Color side;
  final Color surface;
  final Color raised;
  final Color line;
  final Color text;
  final Color text2;
  final Color muted;
  final Color brand;
  final Color brandInk;
  final Color userBubble;
  final Color codeBg;
  final Color danger;
  final Color warn;
  final Color ok;
  final Color kindChat;
  final Color kindCode;
  final Color kindArtifact;
  final Color kindProject;
  final Color kindScheduled;

  static const dark = AsgColors(
    bg: Color(0xFF161614),
    side: Color(0xFF121211),
    surface: Color(0xFF20201D),
    raised: Color(0xFF282825),
    line: Color(0x18FFFFFF),
    text: Color(0xFFECEBE5),
    text2: Color(0xFFC9C8C0),
    muted: Color(0xFF98978E),
    brand: Color(0xFFC6F432),
    brandInk: Color(0xFF151A02),
    userBubble: Color(0xFF2B2B27),
    codeBg: Color(0xFF10100E),
    danger: Color(0xFFFF6B5E),
    warn: Color(0xFFF5B84A),
    ok: Color(0xFF7EE0A8),
    kindChat: Color(0xFF8FB2FF),
    kindCode: Color(0xFF74E0A6),
    kindArtifact: Color(0xFFC4A0FF),
    kindProject: Color(0xFFFFB468),
    kindScheduled: Color(0xFF66D4E6),
  );

  static const light = AsgColors(
    bg: Color(0xFFF7F6F1),
    side: Color(0xFFECEBE4),
    surface: Color(0xFFFFFFFF),
    raised: Color(0xFFF3F2EC),
    line: Color(0x1814140A),
    text: Color(0xFF1D1D1A),
    text2: Color(0xFF3B3B36),
    muted: Color(0xFF6F6E66),
    brand: Color(0xFFC6F432),
    brandInk: Color(0xFF1B2300),
    userBubble: Color(0xFFECEBE3),
    codeBg: Color(0xFF181815),
    danger: Color(0xFFD4402F),
    warn: Color(0xFFB77600),
    ok: Color(0xFF1F8A4C),
    kindChat: Color(0xFF3563D9),
    kindCode: Color(0xFF158A4F),
    kindArtifact: Color(0xFF7C45D6),
    kindProject: Color(0xFFC46A10),
    kindScheduled: Color(0xFF0F8798),
  );

  Color colorForKind(String kind) {
    switch (kind) {
      case 'code':
        return kindCode;
      case 'artifact':
        return kindArtifact;
      case 'project':
        return kindProject;
      case 'scheduled':
        return kindScheduled;
      default:
        return kindChat;
    }
  }

  static IconData iconForKind(String kind) {
    switch (kind) {
      case 'code':
        return Icons.terminal_rounded;
      case 'artifact':
        return Icons.auto_awesome_rounded;
      case 'project':
        return Icons.folder_rounded;
      case 'scheduled':
        return Icons.schedule_rounded;
      default:
        return Icons.chat_bubble_outline_rounded;
    }
  }

  @override
  AsgColors copyWith({
    Color? bg,
    Color? side,
    Color? surface,
    Color? raised,
    Color? line,
    Color? text,
    Color? text2,
    Color? muted,
    Color? brand,
    Color? brandInk,
    Color? userBubble,
    Color? codeBg,
    Color? danger,
    Color? warn,
    Color? ok,
    Color? kindChat,
    Color? kindCode,
    Color? kindArtifact,
    Color? kindProject,
    Color? kindScheduled,
  }) {
    return AsgColors(
      bg: bg ?? this.bg,
      side: side ?? this.side,
      surface: surface ?? this.surface,
      raised: raised ?? this.raised,
      line: line ?? this.line,
      text: text ?? this.text,
      text2: text2 ?? this.text2,
      muted: muted ?? this.muted,
      brand: brand ?? this.brand,
      brandInk: brandInk ?? this.brandInk,
      userBubble: userBubble ?? this.userBubble,
      codeBg: codeBg ?? this.codeBg,
      danger: danger ?? this.danger,
      warn: warn ?? this.warn,
      ok: ok ?? this.ok,
      kindChat: kindChat ?? this.kindChat,
      kindCode: kindCode ?? this.kindCode,
      kindArtifact: kindArtifact ?? this.kindArtifact,
      kindProject: kindProject ?? this.kindProject,
      kindScheduled: kindScheduled ?? this.kindScheduled,
    );
  }

  @override
  AsgColors lerp(ThemeExtension<AsgColors>? other, double t) {
    if (other is! AsgColors) return this;
    return AsgColors(
      bg: Color.lerp(bg, other.bg, t)!,
      side: Color.lerp(side, other.side, t)!,
      surface: Color.lerp(surface, other.surface, t)!,
      raised: Color.lerp(raised, other.raised, t)!,
      line: Color.lerp(line, other.line, t)!,
      text: Color.lerp(text, other.text, t)!,
      text2: Color.lerp(text2, other.text2, t)!,
      muted: Color.lerp(muted, other.muted, t)!,
      brand: Color.lerp(brand, other.brand, t)!,
      brandInk: Color.lerp(brandInk, other.brandInk, t)!,
      userBubble: Color.lerp(userBubble, other.userBubble, t)!,
      codeBg: Color.lerp(codeBg, other.codeBg, t)!,
      danger: Color.lerp(danger, other.danger, t)!,
      warn: Color.lerp(warn, other.warn, t)!,
      ok: Color.lerp(ok, other.ok, t)!,
      kindChat: Color.lerp(kindChat, other.kindChat, t)!,
      kindCode: Color.lerp(kindCode, other.kindCode, t)!,
      kindArtifact: Color.lerp(kindArtifact, other.kindArtifact, t)!,
      kindProject: Color.lerp(kindProject, other.kindProject, t)!,
      kindScheduled: Color.lerp(kindScheduled, other.kindScheduled, t)!,
    );
  }
}

extension AsgThemeContext on BuildContext {
  AsgColors get asg => Theme.of(this).extension<AsgColors>() ?? AsgColors.dark;
}

ThemeData buildAsgTheme(Brightness brightness) {
  final c = brightness == Brightness.dark ? AsgColors.dark : AsgColors.light;
  final scheme = ColorScheme.fromSeed(
    seedColor: c.brand,
    brightness: brightness,
    surface: c.bg,
    primary: c.brand,
    onPrimary: c.brandInk,
  );
  return ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor: c.bg,
    appBarTheme: AppBarTheme(
      backgroundColor: c.bg,
      foregroundColor: c.text,
      elevation: 0,
      scrolledUnderElevation: 0,
      surfaceTintColor: Colors.transparent,
    ),
    drawerTheme: DrawerThemeData(
      backgroundColor: c.side,
      surfaceTintColor: Colors.transparent,
    ),
    dividerColor: c.line,
    extensions: [c],
  );
}
