import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../theme/asg_theme.dart';

/// Lightweight Markdown renderer for assistant replies: headings, lists,
/// inline bold/code, and dark copyable fenced code blocks.
class SimpleMarkdown extends StatelessWidget {
  const SimpleMarkdown({super.key, required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final blocks = _parseBlocks(text);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var i = 0; i < blocks.length; i++) ...[
          if (i > 0) const SizedBox(height: 8),
          _buildBlock(context, blocks[i]),
        ],
      ],
    );
  }

  Widget _buildBlock(BuildContext context, _MdBlock block) {
    final c = context.asg;
    if (block.kind == _BlockKind.code) {
      return _CodeCard(lang: block.lang, code: block.text);
    }
    if (block.kind == _BlockKind.heading) {
      final size = block.level == 1 ? 19.0 : (block.level == 2 ? 16.5 : 15.0);
      return SelectableText.rich(
        _inlineSpan(
          block.text,
          TextStyle(
            fontSize: size,
            fontWeight: FontWeight.w700,
            color: c.text,
            height: 1.3,
          ),
          c,
        ),
      );
    }
    if (block.kind == _BlockKind.listItem) {
      return Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 2, right: 8),
            child: Text(
              block.bullet,
              style: TextStyle(
                color: c.brand,
                fontWeight: FontWeight.w700,
                fontSize: 14,
              ),
            ),
          ),
          Expanded(
            child: SelectableText.rich(
              _inlineSpan(
                block.text,
                TextStyle(fontSize: 15, height: 1.5, color: c.text),
                c,
              ),
            ),
          ),
        ],
      );
    }
    return SelectableText.rich(
      _inlineSpan(
        block.text,
        TextStyle(fontSize: 15, height: 1.55, color: c.text),
        c,
      ),
    );
  }

  TextSpan _inlineSpan(String src, TextStyle base, AsgColors c) {
    final spans = <InlineSpan>[];
    final pattern = RegExp(r'(`[^`]+`|\*\*[^*]+\*\*)');
    var start = 0;
    for (final m in pattern.allMatches(src)) {
      if (m.start > start) {
        spans.add(TextSpan(text: src.substring(start, m.start)));
      }
      final tok = m.group(0)!;
      if (tok.startsWith('`')) {
        spans.add(
          TextSpan(
            text: tok.substring(1, tok.length - 1),
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: (base.fontSize ?? 15) * 0.88,
              backgroundColor: c.raised,
              color: c.text,
            ),
          ),
        );
      } else if (tok.startsWith('**')) {
        spans.add(
          TextSpan(
            text: tok.substring(2, tok.length - 2),
            style: const TextStyle(fontWeight: FontWeight.w700),
          ),
        );
      }
      start = m.end;
    }
    if (start < src.length) {
      spans.add(TextSpan(text: src.substring(start)));
    }
    return TextSpan(style: base, children: spans);
  }

  List<_MdBlock> _parseBlocks(String src) {
    final lines = src.replaceAll('\r\n', '\n').split('\n');
    final out = <_MdBlock>[];
    final para = <String>[];

    void flushPara() {
      if (para.isEmpty) return;
      out.add(_MdBlock(_BlockKind.paragraph, para.join('\n').trim()));
      para.clear();
    }

    var i = 0;
    while (i < lines.length) {
      final line = lines[i];
      if (line.trimLeft().startsWith('```')) {
        flushPara();
        final lang = line.trimLeft().substring(3).trim();
        final buf = <String>[];
        i++;
        while (i < lines.length && !lines[i].trimLeft().startsWith('```')) {
          buf.add(lines[i]);
          i++;
        }
        if (i < lines.length) i++;
        out.add(_MdBlock(_BlockKind.code, buf.join('\n'), lang: lang));
        continue;
      }
      if (line.trim().isEmpty) {
        flushPara();
        i++;
        continue;
      }
      final h = RegExp(r'^(#{1,3})\s+(.+)$').firstMatch(line);
      if (h != null) {
        flushPara();
        out.add(
          _MdBlock(
            _BlockKind.heading,
            h.group(2)!.trim(),
            level: h.group(1)!.length,
          ),
        );
        i++;
        continue;
      }
      final li = RegExp(r'^\s*([-*+]|\d+\.)\s+(.+)$').firstMatch(line);
      if (li != null) {
        flushPara();
        final marker = li.group(1)!;
        out.add(
          _MdBlock(
            _BlockKind.listItem,
            li.group(2)!.trim(),
            bullet: marker.endsWith('.') ? marker : '•',
          ),
        );
        i++;
        continue;
      }
      para.add(line);
      i++;
    }
    flushPara();
    return out;
  }
}

enum _BlockKind { paragraph, heading, listItem, code }

class _MdBlock {
  const _MdBlock(
    this.kind,
    this.text, {
    this.lang = '',
    this.level = 1,
    this.bullet = '•',
  });

  final _BlockKind kind;
  final String text;
  final String lang;
  final int level;
  final String bullet;
}

class _CodeCard extends StatefulWidget {
  const _CodeCard({required this.lang, required this.code});

  final String lang;
  final String code;

  @override
  State<_CodeCard> createState() => _CodeCardState();
}

class _CodeCardState extends State<_CodeCard> {
  bool _copied = false;

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.code));
    if (!mounted) return;
    setState(() => _copied = true);
    Future<void>.delayed(const Duration(milliseconds: 1400), () {
      if (mounted) setState(() => _copied = false);
    });
  }

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: c.codeBg,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: c.line),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            decoration: BoxDecoration(
              border: Border(bottom: BorderSide(color: c.line)),
            ),
            child: Row(
              children: [
                Text(
                  widget.lang.isEmpty ? 'code' : widget.lang,
                  style: TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 11,
                    color: c.muted,
                  ),
                ),
                const Spacer(),
                InkWell(
                  onTap: _copy,
                  borderRadius: BorderRadius.circular(6),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 2,
                    ),
                    child: Text(
                      _copied ? 'Copied' : 'Copy',
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 11,
                        fontWeight: FontWeight.w600,
                        color: _copied ? c.brand : c.text2,
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.all(12),
            child: SelectableText(
              widget.code,
              style: const TextStyle(
                fontFamily: 'monospace',
                fontSize: 12.5,
                height: 1.5,
                color: Color(0xFFECEBE5),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
