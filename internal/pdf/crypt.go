package pdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
)

// Many PDFs are encrypted only to set permissions: they open without a
// password because the user password is empty. Others have an empty owner
// password, which any library that tries it (poppler does) can use to
// open them. Sphinx tries both with the standard security handler
// (revisions 2 to 6), as anyone receiving the file could. Files that need
// a real password stay unreadable.

var padding = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

type method int

const (
	none method = iota
	useRC4
	useAES
)

type decrypter struct {
	key             []byte
	r               int
	streams         method
	strings         method
	encryptMetadata bool
	encryptDict     Object
}

func newDecrypter(d *Document) *decrypter {
	enc := d.Dict(d.trailer["Encrypt"])
	if enc == nil || enc.Name("Filter") != "Standard" {
		return nil
	}
	num := func(k Name, def int) int {
		if v, ok := d.Num(enc[k]); ok {
			return int(v)
		}
		return def
	}
	str := func(k Name) []byte {
		s, _ := d.Resolve(enc[k]).(String)
		return s
	}
	c := &decrypter{r: num("R", 2), encryptMetadata: true, encryptDict: d.trailer["Encrypt"]}
	if v, ok := d.Resolve(enc["EncryptMetadata"]).(bool); ok {
		c.encryptMetadata = v
	}
	v := num("V", 0)
	c.streams, c.strings = useRC4, useRC4
	if v >= 4 {
		cf := d.Dict(enc["CF"])
		c.streams = filterMethod(d, cf, enc.Name("StmF"))
		c.strings = filterMethod(d, cf, enc.Name("StrF"))
	}
	o, u := str("O"), str("U")
	switch {
	case c.r >= 5:
		c.key = keyR6(c.r, u, str("UE"))
		if c.key == nil {
			c.key = ownerKeyR6(c.r, o, u, str("OE"))
		}
	case len(o) >= 32 && len(u) >= 16:
		var id []byte
		if ids, ok := d.Resolve(d.trailer["ID"]).(Array); ok && len(ids) > 0 {
			id, _ = d.Resolve(ids[0]).(String)
		}
		bits, p := num("Length", 40), uint32(num("P", 0))
		c.key = keyR4(c.r, bits, padding, o, u, p, id, c.encryptMetadata)
		if c.key == nil {
			c.key = keyR4(c.r, bits, userFromOwner(c.r, bits, o), o, u, p, id, c.encryptMetadata)
		}
	}
	if c.key == nil {
		// Files that encrypt only their attachments (StmF and StrF set to
		// Identity) open without a key; their pages are in the clear.
		if c.streams == none && c.strings == none {
			return c
		}
		return nil
	}
	return c
}

func filterMethod(d *Document, cf Dict, name Name) method {
	if name == "Identity" || name == "" {
		return none
	}
	switch d.Dict(cf[name]).Name("CFM") {
	case "AESV2", "AESV3":
		return useAES
	case "V2":
		return useRC4
	}
	return none
}

// keyR4 computes the file key for revisions 2 to 4 from the empty user
// password (algorithm 2 of ISO 32000) and checks it against /U.
func keyR4(r, bits int, password, o, u []byte, p uint32, id []byte, encryptMetadata bool) []byte {
	n := keyLen(r, bits)
	h := md5.New()
	h.Write(password)
	h.Write(o[:32])
	h.Write(binary.LittleEndian.AppendUint32(nil, p))
	h.Write(id)
	if r >= 4 && !encryptMetadata {
		h.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}
	key := h.Sum(nil)[:n]
	if r >= 3 {
		for range 50 {
			sum := md5.Sum(key)
			key = sum[:n]
		}
	}
	var check []byte
	if r == 2 {
		check = rc4XOR(key, padding)
		if !bytes.Equal(check, u[:32]) {
			return nil
		}
		return key
	}
	sum := md5.Sum(append(append([]byte(nil), padding...), id...))
	check = sum[:]
	for i := 0; i < 20; i++ {
		k := make([]byte, len(key))
		for j := range key {
			k[j] = key[j] ^ byte(i)
		}
		check = rc4XOR(k, check)
	}
	if !bytes.Equal(check, u[:16]) {
		return nil
	}
	return key
}

func keyLen(r, bits int) int {
	if r >= 3 {
		return max(5, min(16, bits/8))
	}
	return 5
}

// userFromOwner recovers the padded user password from /O, assuming an
// empty owner password (algorithm 7 of ISO 32000).
func userFromOwner(r, bits int, o []byte) []byte {
	n := keyLen(r, bits)
	sum := md5.Sum(padding)
	key := sum[:n]
	if r >= 3 {
		for range 50 {
			s := md5.Sum(key)
			key = s[:n]
		}
	}
	if r == 2 {
		return rc4XOR(key, o[:32])
	}
	out := append([]byte(nil), o[:32]...)
	for i := 19; i >= 0; i-- {
		k := make([]byte, len(key))
		for j := range key {
			k[j] = key[j] ^ byte(i)
		}
		out = rc4XOR(k, out)
	}
	return out
}

// ownerKeyR6 derives the file key from an empty owner password.
func ownerKeyR6(r int, o, u, oe []byte) []byte {
	if len(o) < 48 || len(u) < 48 || len(oe) < 32 {
		return nil
	}
	if !bytes.Equal(hashR6(r, nil, o[32:40], u[:48]), o[:32]) {
		return nil
	}
	return decryptKey(hashR6(r, nil, o[40:48], u[:48]), oe)
}

func decryptKey(inter, wrapped []byte) []byte {
	block, err := aes.NewCipher(inter)
	if err != nil {
		return nil
	}
	key := make([]byte, 32)
	cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(key, wrapped[:32])
	return key
}

// keyR6 derives the AES-256 file key of revisions 5 and 6 from the empty
// user password, after validating it against the hash stored in /U.
func keyR6(r int, u, ue []byte) []byte {
	if len(u) < 48 || len(ue) < 32 {
		return nil
	}
	validation, keySalt := u[32:40], u[40:48]
	if !bytes.Equal(hashR6(r, nil, validation, nil), u[:32]) {
		return nil
	}
	return decryptKey(hashR6(r, nil, keySalt, nil), ue)
}

// hashR6 is algorithm 2.B of ISO 32000-2; revision 5 used a single SHA-256.
func hashR6(r int, password, salt, udata []byte) []byte {
	sum := sha256.Sum256(append(append(append([]byte(nil), password...), salt...), udata...))
	k := sum[:]
	if r == 5 {
		return k
	}
	for i := 0; ; i++ {
		var k1 []byte
		for range 64 {
			k1 = append(k1, password...)
			k1 = append(k1, k...)
			k1 = append(k1, udata...)
		}
		block, _ := aes.NewCipher(k[:16])
		e := make([]byte, len(k1))
		cipher.NewCBCEncrypter(block, k[16:32]).CryptBlocks(e, k1)
		var mod int
		for _, b := range e[:16] {
			mod += int(b)
		}
		switch mod % 3 {
		case 0:
			s := sha256.Sum256(e)
			k = s[:]
		case 1:
			s := sha512.Sum384(e)
			k = s[:]
		default:
			s := sha512.Sum512(e)
			k = s[:]
		}
		if rounds := i + 1; rounds >= 64 && int(e[len(e)-1]) <= rounds-32 {
			return k[:32]
		}
	}
}

func rc4XOR(key, data []byte) []byte {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

func (c *decrypter) objectKey(ref Ref, m method) []byte {
	if c.r >= 5 {
		return c.key
	}
	k := append([]byte(nil), c.key...)
	k = append(k, byte(ref.Num), byte(ref.Num>>8), byte(ref.Num>>16), byte(ref.Gen), byte(ref.Gen>>8))
	if m == useAES {
		k = append(k, "sAlT"...)
	}
	sum := md5.Sum(k)
	return sum[:min(len(c.key)+5, 16)]
}

func (c *decrypter) bytes(b []byte, ref Ref, m method) []byte {
	switch m {
	case useRC4:
		return rc4XOR(c.objectKey(ref, m), b)
	case useAES:
		if len(b) < 32 || len(b)%16 != 0 {
			return nil
		}
		block, err := aes.NewCipher(c.objectKey(ref, m))
		if err != nil {
			return nil
		}
		out := make([]byte, len(b)-16)
		cipher.NewCBCDecrypter(block, b[:16]).CryptBlocks(out, b[16:])
		if p := int(out[len(out)-1]); p >= 1 && p <= 16 && p <= len(out) {
			out = out[:len(out)-p]
		}
		return out
	}
	return b
}

// object returns a decrypted copy of an indirect object. The encryption
// dictionary itself, cross-reference streams and, when asked, the
// metadata stream are stored in the clear.
func (c *decrypter) object(o Object, ref Ref) Object {
	if r, ok := c.encryptDict.(Ref); ok && r == ref {
		return o
	}
	if s, ok := o.(*Stream); ok {
		t := s.Dict.Name("Type")
		if t == "XRef" || (t == "Metadata" && !c.encryptMetadata) {
			return o
		}
		return &Stream{Dict: c.value(s.Dict, ref).(Dict), Raw: c.bytes(s.Raw, ref, c.streams)}
	}
	return c.value(o, ref)
}

func (c *decrypter) value(o Object, ref Ref) Object {
	switch v := o.(type) {
	case String:
		return String(c.bytes(v, ref, c.strings))
	case Array:
		out := make(Array, len(v))
		for i, x := range v {
			out[i] = c.value(x, ref)
		}
		return out
	case Dict:
		out := make(Dict, len(v))
		for k, x := range v {
			out[k] = c.value(x, ref)
		}
		return out
	}
	return o
}
