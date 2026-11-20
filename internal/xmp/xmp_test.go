package xmp

import (
	"reflect"
	"testing"
)

const packet = `junk<?xpacket begin=""?><x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
 <rdf:Description xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:aux="http://ns.adobe.com/exif/1.0/aux/"
   xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
   xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#"
   xmp:CreatorTool="Adobe Photoshop 25.0" aux:SerialNumber="0123456">
  <dc:creator><rdf:Seq><rdf:li>Jane Roe</rdf:li></rdf:Seq></dc:creator>
  <xmpMM:History><rdf:Seq>
   <rdf:li stEvt:action="saved" stEvt:softwareAgent="Adobe Photoshop 25.0"/>
   <rdf:li><rdf:Description><stEvt:action>converted</stEvt:action></rdf:Description></rdf:li>
  </rdf:Seq></xmpMM:History>
 </rdf:Description>
</rdf:RDF></x:xmpmeta>trailing`

func TestParse(t *testing.T) {
	p := Parse([]byte(packet))
	want := Packet{
		"xmp:CreatorTool":     {"Adobe Photoshop 25.0"},
		"aux:SerialNumber":    {"0123456"},
		"dc:creator":          {"Jane Roe"},
		"stEvt:action":        {"saved", "converted"},
		"stEvt:softwareAgent": {"Adobe Photoshop 25.0"},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("got %v", p)
	}
}

func TestParseWithoutPacket(t *testing.T) {
	if p := Parse([]byte("no xml here")); len(p) != 0 {
		t.Fatalf("got %v", p)
	}
}
