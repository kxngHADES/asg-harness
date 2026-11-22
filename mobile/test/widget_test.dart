import 'package:asg_harness/main.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('AsgApp renders home screen', (tester) async {
    await tester.pumpWidget(const AsgApp());
    expect(find.text('ASG'), findsOneWidget);
  });
}
