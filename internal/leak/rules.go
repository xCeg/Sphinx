package leak

// Rule is a kind of leak. Why is the plain-language explanation shown by
// `sphinx rules` and on the website; Fix tells the reader what to do.
type Rule struct {
	ID       string
	Name     string
	Severity Severity
	Title    string
	Why      string
	Fix      string
	Formats  []string
	Seal     bool
	Reveal   bool
}

var (
	HiddenCrop = &Rule{
		ID: "SPX001", Name: "hidden-crop", Severity: Critical,
		Title:   "Cropped-out pixels are still in the file",
		Why:     "Some editors save a cropped picture by writing it over the original without shortening the file. The visible image is the cropped one, but the bottom of the original is still stored after it, and can be turned back into pixels. This is the aCropalypse bug (CVE-2023-21036 on Google Pixel, CVE-2023-28303 in the Windows Snipping Tool).",
		Fix:     "Run `sphinx seal` on the file, or re-export it from a fixed editor. Update the phone or the Snipping Tool.",
		Formats: []string{"png"}, Seal: true, Reveal: true,
	}
	BlackedOutText = &Rule{
		ID: "SPX002", Name: "blacked-out-text", Severity: Critical,
		Title:   "Blacked-out text is still in the file",
		Why:     "A box was drawn on top of the text, but the text itself was never removed. Anyone can select it, copy it or extract it, even though it looks hidden on screen and on paper.",
		Fix:     "Redact with a tool that removes the text (a true redaction feature), then check the result again with Sphinx. Never cover text with shapes.",
		Formats: []string{"pdf"}, Reveal: true,
	}
	UnappliedRedaction = &Rule{
		ID: "SPX003", Name: "unapplied-redaction", Severity: Critical,
		Title:   "Redaction marked but never applied",
		Why:     "The document contains redaction marks that were never applied. The marked text is still fully present underneath.",
		Fix:     "Open the document in the editor that created the marks and apply the redactions, then save a new copy.",
		Formats: []string{"pdf"}, Reveal: true,
	}
	EarlierRevision = &Rule{
		ID: "SPX004", Name: "earlier-revision", Severity: High,
		Title:   "Earlier versions of the document are recoverable",
		Why:     "PDF editors often save changes by appending them to the end of the file. Every previous version stays inside, including any text that was deleted or edited afterwards.",
		Fix:     "Use \"Save As\" or \"Print to PDF\" to produce a single-version copy, then check it again.",
		Formats: []string{"pdf"}, Reveal: true,
	}
	DeletedText = &Rule{
		ID: "SPX005", Name: "deleted-text", Severity: High,
		Title:   "Deleted text is still in the document",
		Why:     "Track Changes is on. Text you deleted is not gone; it is stored as a pending deletion that anyone can display.",
		Fix:     "In Word: Review > Accept All Changes, then remove comments, then save.",
		Formats: []string{"docx"}, Reveal: true,
	}
	GPSLocation = &Rule{
		ID: "SPX006", Name: "gps-location", Severity: High,
		Title:   "The exact place the photo was taken",
		Why:     "Phones write the GPS position into each photo. A photo taken at home gives away your address to anyone who receives the original file.",
		Fix:     "Run `sphinx seal`, or turn off location tagging in the camera settings.",
		Formats: []string{"jpeg", "png"}, Seal: true,
	}
	ThumbnailMismatch = &Rule{
		ID: "SPX007", Name: "thumbnail-mismatch", Severity: High,
		Title:   "The preview thumbnail shows a different picture",
		Why:     "Photos carry a small preview image. Some editors crop or retouch the main picture but leave the old preview untouched, so it still shows what you removed.",
		Fix:     "Run `sphinx seal`, which drops the stale preview.",
		Formats: []string{"jpeg"}, Seal: true, Reveal: true,
	}
	HiddenVideo = &Rule{
		ID: "SPX008", Name: "hidden-video", Severity: High,
		Title:   "A video is hidden inside the photo",
		Why:     "\"Motion\" or \"Live\" photos store a short video, often with sound, recorded around the moment you pressed the shutter. It travels with the photo and can show and say more than the picture.",
		Fix:     "Run `sphinx seal`, or turn off motion photos before sharing.",
		Formats: []string{"jpeg"}, Seal: true, Reveal: true,
	}
	TrailingData = &Rule{
		ID: "SPX009", Name: "trailing-data", Severity: Medium,
		Title:   "Extra data after the end of the image",
		Why:     "Something is stored after the point where the image ends. Viewers ignore it, but it is sent along with the file.",
		Fix:     "Run `sphinx seal`, which cuts the file at the real end of the image.",
		Formats: []string{"jpeg", "png"}, Seal: true, Reveal: true,
	}
	HiddenContent = &Rule{
		ID: "SPX010", Name: "hidden-content", Severity: Medium,
		Title:   "Content hidden from view",
		Why:     "Hidden text, hidden sheets, hidden slides and speaker notes do not show when you read the document, but they are sent with it.",
		Fix:     "Unhide and review the content, delete what should not be shared, or export to PDF.",
		Formats: []string{"docx", "xlsx", "pptx"}, Reveal: true,
	}
	Comments = &Rule{
		ID: "SPX011", Name: "comments", Severity: Medium,
		Title:   "Review comments",
		Why:     "Margin comments, with the name of whoever wrote them, are part of the file even when they are not displayed.",
		Fix:     "In Office: Review > Delete All Comments, then save.",
		Formats: []string{"docx", "xlsx", "pptx", "pdf"}, Reveal: true,
	}
	PersonalNames = &Rule{
		ID: "SPX012", Name: "personal-names", Severity: Medium,
		Title:   "Names of the people who made or edited the file",
		Why:     "Editors record the author and the last person who saved the file, usually taken from the computer account.",
		Fix:     "Run `sphinx seal` on images and Office files. For PDF, clear the properties in the editor before exporting.",
		Formats: []string{"jpeg", "png", "pdf", "docx", "xlsx", "pptx"}, Seal: true,
	}
	DeviceSerial = &Rule{
		ID: "SPX013", Name: "device-serial", Severity: Medium,
		Title:   "The serial number of your camera",
		Why:     "Cameras write their own serial number into every photo. It links photos you publish under different names to the same device.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png"}, Seal: true,
	}
	LocalPaths = &Rule{
		ID: "SPX014", Name: "local-paths", Severity: Medium,
		Title:   "Paths from your computer",
		Why:     "File paths such as C:\\Users\\alice\\... reveal your account name, how your disk is organised, and sometimes client or project names.",
		Fix:     "Run `sphinx seal` on Office files. For PDF, export again from a clean location.",
		Formats: []string{"docx", "xlsx", "pptx", "pdf"}, Seal: true,
	}
	EmbeddedFiles = &Rule{
		ID: "SPX015", Name: "embedded-files", Severity: Medium,
		Title:   "Other files are attached inside",
		Why:     "The document carries attachments or embedded objects (spreadsheets, images, other documents) that are sent along with it and may contain more than what is shown.",
		Fix:     "Open the embedded objects, check them, and remove the ones that should not travel.",
		Formats: []string{"pdf", "docx", "xlsx", "pptx"}, Reveal: true,
	}
	EmbeddedText = &Rule{
		ID: "SPX016", Name: "embedded-text", Severity: Low,
		Title:   "Text notes stored inside the image",
		Why:     "Images can carry free text: comments, descriptions, or the full prompt and settings used to generate an AI picture.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png"}, Seal: true,
	}
	Timestamps = &Rule{
		ID: "SPX017", Name: "timestamps", Severity: Low,
		Title:   "When the file was created and edited",
		Why:     "Exact dates and times can contradict a story, or reveal working hours and time zone.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png", "pdf", "docx", "xlsx", "pptx"}, Seal: true,
	}
	SoftwareTrail = &Rule{
		ID: "SPX018", Name: "software-trail", Severity: Low,
		Title:   "The software, devices and edit history behind the file",
		Why:     "Programs leave their name, version and sometimes a step-by-step editing history. It can show that an image was retouched, or identify your device.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png", "pdf", "docx", "xlsx", "pptx"}, Seal: true,
	}
)

// Rules lists every rule in reference order.
var Rules = []*Rule{
	HiddenCrop, BlackedOutText, UnappliedRedaction, EarlierRevision, DeletedText,
	GPSLocation, ThumbnailMismatch, HiddenVideo, TrailingData, HiddenContent,
	Comments, PersonalNames, DeviceSerial, LocalPaths, EmbeddedFiles,
	EmbeddedText, Timestamps, SoftwareTrail,
}

func Lookup(idOrName string) *Rule {
	for _, r := range Rules {
		if r.ID == idOrName || r.Name == idOrName {
			return r
		}
	}
	return nil
}
