package main

import (
	"bytes"
	"log"
)

/* taken from C code located at http://www.scard.org/gsm/a51.html */

var (
	R1Mask   uint32 = 0x07FFFF /* 19 bits */
	R2Mask   uint32 = 0x3FFFFF /* 22 bits */
	R3Mask   uint32 = 0x7FFFFF /* 23 bits */
	R1MidBit uint32 = 0x000100 /* bit 8 */
	R2MidBit uint32 = 0x000400 /* bit 10 */
	R3MidBit uint32 = 0x000400 /* bit 10 */
)

var (
	R1Taps uint32 = 0x072000 /* bits 18,17,16,13 */
	R2Taps uint32 = 0x300000 /* bits 21,20 */
	R3Taps uint32 = 0x700080 /* bits 22,21,20,7 */
	R1Out  uint32 = 0x040000 /* bit 18 (the high bit) */
	R2Out  uint32 = 0x200000 /* bit 21 (the high bit) */
	R3Out  uint32 = 0x400000 /* bit 22 (the high bit) */
)

/* calculate the parity of a 32-bit word */
/* i.e. sum of all its bits modulo 2 */
func parity(x uint32) uint32 {
	x ^= x >> 16
	x ^= x >> 8
	x ^= x >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x & 1
}

/* clock a register */
func clockRegister(register, mask, taps uint32) uint32 {
	t := register & taps
	register = (register << 1) & mask
	register |= parity(t)

	return register
}

/* check the middle bit of each register and return the majority value */
func majority(R1, R2, R3 uint32) uint32 {
	if parity(R1&R1MidBit)+parity(R2&R2MidBit)+parity(R3&R3MidBit) >= 2 {
		return 1
	}

	return 0
}

/* clock all three registers with specific clock control:
 *    aka clock R# whenever R#'s middle bit agrees with the
 *        majority of middle bits
 */
func clock(R1, R2, R3 uint32) (uint32, uint32, uint32) {
	maj := majority(R1, R2, R3)
	if (R1&R1MidBit != 0) == (maj > 0) {
		R1 = clockRegister(R1, R1Mask, R1Taps)
	}
	if (R2&R2MidBit != 0) == (maj > 0) {
		R2 = clockRegister(R2, R2Mask, R2Taps)
	}
	if (R3&R3MidBit != 0) == (maj > 0) {
		R3 = clockRegister(R3, R3Mask, R3Taps)
	}

	return R1, R2, R3
}

/* clock all registers regardless of middle bit majority */
func clockAllThree(R1, R2, R3 uint32) (uint32, uint32, uint32) {
	R1 = clockRegister(R1, R1Mask, R1Taps)
	R2 = clockRegister(R2, R2Mask, R2Taps)
	R3 = clockRegister(R3, R3Mask, R3Taps)

	return R1, R2, R3
}

/* generate an output bit from the current register state:
 *    grab a bit from each register and xor them all together
 */
func getOutputBit(R1, R2, R3 uint32) uint32 {
	return parity(R1&R1Out) ^ parity(R2&R2Out) ^ parity(R3&R3Out)
}

/* setup the A5/1 key.  Accepts a 64-bit key and a 22-bit Frame Number */
func keySetup(key [8]byte, frame uint32) (R1, R2, R3 uint32) {
	var keyBit, frameBit uint32
	var i uint32

	// load the key into the shift registers,
	// LSB of the first byte of the key array
	// first, clocking each register once for
	// every key bit loaded (without worrying
	// about middle bit majority)
	for i = 0; i < 64; i++ {
		R1, R2, R3 = clockAllThree(R1, R2, R3)
		keyBit = uint32((key[i/8] >> (i & 7)) & 1) /* the i-th bit of the key */
		R1 ^= keyBit
		R2 ^= keyBit
		R3 ^= keyBit
	}

	// load the frame number into the shift registers,
	// LSB first, clocking each register once for every
	// key bit loaded (without worrying about middle
	// bit majority)
	for i = 0; i < 22; i++ {
		R1, R2, R3 = clockAllThree(R1, R2, R3)

		frameBit = uint32((frame >> i) & 1) /* the i-th bit of the frame */
		R1 ^= frameBit
		R2 ^= frameBit
		R3 ^= frameBit
	}

	// run the shift registers for 100 clocks to mix the keys
	// we re-enable the majority bit rule from here on
	for i = 0; i < 100; i++ {
		R1, R2, R3 = clock(R1, R2, R3)
	}

	// the key is set up properly
	return
}

/* generate output; we generate 228 bits of keystream output.
 * the first 114 bits is for the A->B frame; the next 114 bits
 * is for the B->A frame.
 */
func run(R1, R2, R3 uint32) (AtoB []byte, BtoA []byte, r1 uint32, r2 uint32, r3 uint32) {
	AtoB = make([]byte, 15)
	BtoA = make([]byte, 15)

	var i uint32

	for i = 0; i < 114; i++ {
		R1, R2, R3 = clock(R1, R2, R3)

		// store the bit MSB first
		AtoB[i/8] |= byte(getOutputBit(R1, R2, R3) << (7 - (i & 7)))
	}

	for i = 0; i < 114; i++ {
		R1, R2, R3 = clock(R1, R2, R3)

		// store the bit MSB first
		BtoA[i/8] |= byte(getOutputBit(R1, R2, R3) << (7 - (i & 7)))
	}

	return AtoB, BtoA, R1, R2, R3
}

func test() {
	key := [8]byte{0x12, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	var frame uint32 = 0x134

	goodAtoB := []byte{0x53, 0x4E, 0xAA, 0x58, 0x2F, 0xE8, 0x15,
		0x1A, 0xB6, 0xE1, 0x85, 0x5A, 0x72, 0x8C, 0x00}
	goodBtoA := []byte{0x24, 0xFD, 0x35, 0xA3, 0x5D, 0x5F, 0xB6,
		0x52, 0x6D, 0x32, 0xF9, 0x06, 0xDF, 0x1A, 0xC0}

	R1, R2, R3 := keySetup(key, frame)

	AtoB, BtoA, R1, R2, R3 := run(R1, R2, R3)

	if bytes.Compare(AtoB, goodAtoB) != 0 {
		log.Fatal("AtoB array didn't match!")
	}

	if bytes.Compare(BtoA, goodBtoA) != 0 {
		log.Fatal("BtoA array didn't match!")
	}

	log.Println("Test Successful!")
}

func main() {
	test()
}
