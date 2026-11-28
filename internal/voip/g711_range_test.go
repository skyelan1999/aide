package voip

import "testing"

// Every input is valid PCM or a valid G.711 codeword; this checks the complete
// finite domain, including extrema omitted by the sampled round-trip test.
func TestG711FullPCMRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		encode   func(int16) byte
		decode   func(byte) int16
		maxError int
	}{{"ulaw", ulaw, unUlaw, 644}, {"alaw", alaw, unAlaw, 512}} {
		t.Run(tc.name, func(t *testing.T) {
			previous := -32768
			for value := -32768; value <= 32767; value++ {
				got := int(tc.decode(tc.encode(int16(value))))
				delta := got - value
				if delta < 0 {
					delta = -delta
				}
				if delta > tc.maxError || got < previous {
					t.Fatalf("PCM %d decoded %d, previous %d, error %d", value, got, previous, delta)
				}
				previous = got
			}
			for code := 0; code < 256; code++ {
				got := tc.encode(tc.decode(byte(code)))
				// mu-law has two zero codewords; the encoder emits positive zero.
				if tc.name == "ulaw" && code == 0x7f && got == 0xff {
					continue
				}
				if got != byte(code) {
					t.Fatalf("code %02x re-encoded as %02x", code, got)
				}
			}
		})
	}
}

func TestG711EndpointCodewords(t *testing.T) {
	for _, tc := range []struct {
		pcm   int16
		mu, a byte
	}{
		{-32768, 0x00, 0x2a}, {0, 0xff, 0xd5}, {32767, 0x80, 0xaa},
	} {
		if ulaw(tc.pcm) != tc.mu || alaw(tc.pcm) != tc.a {
			t.Fatalf("PCM endpoint %d", tc.pcm)
		}
	}
}
