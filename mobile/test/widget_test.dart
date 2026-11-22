import 'package:asg_harness/main.dart';
import 'package:asg_harness/services/asg_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('AsgClient.normalizeBaseUrl handles bare IP, host:port, and URLs', () {
    expect(
      AsgClient.normalizeBaseUrl('192.168.1.42'),
      'http://192.168.1.42:7789',
    );
    expect(
      AsgClient.normalizeBaseUrl('192.168.1.42:8080'),
      'http://192.168.1.42:8080',
    );
    expect(
      AsgClient.normalizeBaseUrl('http://my-pc.local:7789/'),
      'http://my-pc.local:7789',
    );
    expect(() => AsgClient.normalizeBaseUrl('   '), throwsFormatException);
  });

  testWidgets('AsgApp starts on the Connect screen', (tester) async {
    await tester.pumpWidget(const AsgApp());
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('Connect to ASG'), findsOneWidget);
    expect(find.text('Connect'), findsOneWidget);
    expect(find.text('ON YOUR NETWORK'), findsOneWidget);

    // Let the UDP discovery timer finish cleanly before tearing down.
    await tester.pump(const Duration(seconds: 3));
  });
}
