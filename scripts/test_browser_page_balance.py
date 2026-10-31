#!/usr/bin/env python3
"""Run the production Swift text parser with synthetic financial page fixtures."""
import pathlib, subprocess, tempfile
ROOT=pathlib.Path(__file__).resolve().parents[1]
source=(ROOT/'scripts/native-computer-bridge.swift').read_text()
parser=source.split('// PAGE_BALANCE_PARSER_BEGIN:',1)[1].split('\n',1)[1].split('// PAGE_BALANCE_PARSER_END',1)[0]
checks=r'''
func parse(_ text: [String], _ currency: String = "USD") -> (Double,String)? {
 return parsePageBalance(text,label:"Credit remaining",currency:currency,accountMarker:"Organization")
}
let valid=parse(["Total tokens","12345","Credit remaining","$12.50","Synthetic account","Organization"])
assert(valid?.0 == 12.5 && valid?.1 == "Synthetic account")
assert(parse(["Credit remaining","$1,234.50","Synthetic Organization"])?.0 == 1234.5)
assert(parse(["Credit remaining","-$1.00","Synthetic Organization"]) == nil)
assert(parse(["Credit remaining","$-1.00","Synthetic Organization"])?.0 == -1)
assert(parse(["Credit remaining","$1,23","Synthetic Organization"]) == nil)
assert(parse(["Credit remaining","$1e12","Synthetic Organization"]) == nil)
assert(parse(["Credit remaining","$NaN","Synthetic Organization"]) == nil)
assert(parse(["Credit remaining","$1.00"]) == nil)
assert(parse(["Spent","$1.00","Synthetic Organization"]) == nil)
assert(parse(["Credit remaining","¥12.00","Synthetic Organization"],"CNY")?.0 == 12)
assert(parse(["Credit remaining","¥12.00","Synthetic Organization"]) == nil)
print("PASS production Swift parser: 11 synthetic cases")
'''
with tempfile.TemporaryDirectory(prefix='aide-page-balance-') as directory:
 p=pathlib.Path(directory);(p/'main.swift').write_text('import Foundation\n'+parser+checks)
 subprocess.run(['xcrun','swiftc','-module-cache-path',str(ROOT/'.cache/swift-modules'),str(p/'main.swift'),'-o',str(p/'test')],check=True)
 subprocess.run([str(p/'test')],check=True)
