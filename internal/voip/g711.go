package voip

import "math/bits"

func ulaw(value int16) byte {
	n := int(value)
	mask := byte(0xff)
	if n < 0 {
		n = -n
		mask = 0x7f
	}
	n = min(n, 32635) + 132
	segment := bits.Len(uint(n)) - 8
	return byte(segment<<4|(n>>(segment+3))&15) ^ mask
}

func unUlaw(value byte) int16 {
	v := ^value
	n := (int(v&15)<<3 + 132) << ((v >> 4) & 7)
	if v&128 != 0 {
		return int16(132 - n)
	}
	return int16(n - 132)
}

func alaw(value int16) byte {
	n := int(value)
	mask := byte(0xd5)
	if n < 0 {
		n = -n - 1
		mask = 0x55
	}
	segment := max(0, bits.Len(uint(n))-8)
	return byte(segment<<4|(n>>(max(segment, 1)+3))&15) ^ mask
}

func unAlaw(value byte) int16 {
	v := value ^ 0x55
	n, segment := int(v&15)<<4, (v>>4)&7
	if segment == 0 {
		n += 8
	} else {
		n = (n + 264) << (segment - 1)
	}
	if v&128 == 0 {
		n = -n
	}
	return int16(n)
}
