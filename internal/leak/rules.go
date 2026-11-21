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
		Fix:     "Run `sphinx seal`, which writes the final version alone, or use \"Save As\" in your editor.",
		Formats: []string{"pdf"}, Reveal: true, Seal: true,
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
		Title:   "Where the photo or video was taken",
		Why:     "Phones write the GPS position into each photo. A photo taken at home gives away your address to anyone who receives the original file.",
		Fix:     "Run `sphinx seal`, or turn off location tagging in the camera settings.",
		Formats: []string{"jpeg", "png", "webp", "svg"}, Seal: true,
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
		Formats: []string{"jpeg", "png", "webp"}, Seal: true, Reveal: true,
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
		Formats: []string{"pdf", "docx", "xlsx", "pptx"}, Reveal: true,
	}
	PersonalNames = &Rule{
		ID: "SPX012", Name: "personal-names", Severity: Medium,
		Title:   "Names of the people who made or edited the file",
		Why:     "Editors record the author and the last person who saved the file, usually taken from the computer account.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png", "webp", "svg", "pdf", "docx", "xlsx", "pptx"}, Seal: true,
	}
	DeviceSerial = &Rule{
		ID: "SPX013", Name: "device-serial", Severity: Medium,
		Title:   "The serial number of your camera",
		Why:     "Cameras write their own serial number into every photo. It links photos you publish under different names to the same device.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png", "webp"}, Seal: true,
	}
	LocalPaths = &Rule{
		ID: "SPX014", Name: "local-paths", Severity: Medium,
		Title:   "Paths from your computer",
		Why:     "File paths such as C:\\Users\\alice\\... reveal your account name, how your disk is organised, and sometimes client or project names.",
		Fix:     "Run `sphinx seal`, which removes paths from metadata, templates and links to local files. Links in PDFs stay; remove them in the editor.",
		Formats: []string{"pdf", "docx", "xlsx", "pptx", "svg", "zip"}, Seal: true,
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
		Formats: []string{"jpeg", "png", "webp", "svg"}, Seal: true,
	}
	Timestamps = &Rule{
		ID: "SPX017", Name: "timestamps", Severity: Low,
		Title:   "When the file was created and edited",
		Why:     "Exact dates and times can contradict a story, or reveal working hours and time zone.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png", "webp", "pdf", "docx", "xlsx", "pptx"}, Seal: true,
	}
	SoftwareTrail = &Rule{
		ID: "SPX018", Name: "software-trail", Severity: Low,
		Title:   "The software, devices and edit history behind the file",
		Why:     "Programs leave their name, version and sometimes a step-by-step editing history. It can show that an image was retouched, or identify your device.",
		Fix:     "Run `sphinx seal`.",
		Formats: []string{"jpeg", "png", "webp", "svg", "pdf", "docx", "xlsx", "pptx"}, Seal: true,
	}
)

var (
	HiddenInstructions = &Rule{
		ID: "SPX019", Name: "hidden-instructions", Severity: High,
		Title:   "Hidden text addressed to AI tools",
		Why:     "Text that people cannot see, but that a chatbot or screening tool reading the file will, contains instructions such as \"ignore previous instructions\". It is used to steer AI reviewers of papers and CVs, and it can also be planted in a document someone sends you so that your assistant acts on it.",
		Fix:     "If you wrote it, remove it: it is easy to find and is treated as misconduct by many conferences and employers. If you received the file, do not hand it to an AI tool before removing the hidden text.",
		Formats: []string{"pdf", "docx", "pptx", "svg"}, Reveal: true,
	}
	InvisibleText = &Rule{
		ID: "SPX020", Name: "invisible-text", Severity: Medium,
		Title:   "Text that readers cannot see",
		Why:     "Some text in the file is drawn so that nobody sees it: white on a white page, in a microscopic font, outside the page, clipped out of view, in an invisible rendering mode or in a layer that is switched off. Search engines, copy and paste and AI tools still read it.",
		Fix:     "Find out where it comes from: a template, a pasted block, a hidden layer. Delete it at the source, then export again.",
		Formats: []string{"pdf", "docx", "pptx", "svg"}, Reveal: true,
	}
	FormData = &Rule{
		ID: "SPX021", Name: "form-data", Severity: Medium,
		Title:   "Values typed into form fields",
		Why:     "The document is a form, and its fields hold values. Some fields are hidden or never printed, and values can survive in fields that the visible pages no longer show.",
		Fix:     "Flatten the form (\"Print to PDF\" or your editor's flatten command) or clear the fields that should not travel.",
		Formats: []string{"pdf"}, Reveal: true,
	}
	ContentCredentials = &Rule{
		ID: "SPX022", Name: "content-credentials", Severity: Low,
		Title:   "A signed history of how the image was made",
		Why:     "The image carries Content Credentials (C2PA): a signed record of the device, software and edits behind it, sometimes with the name of the person or organisation that signed it. It is meant to prove authenticity, so it is designed to travel with the file.",
		Fix:     "Keep it if you want to prove where the image comes from. Otherwise `sphinx seal` removes it.",
		Formats: []string{"jpeg", "png", "webp"}, Seal: true,
	}
	SenderNetwork = &Rule{
		ID: "SPX024", Name: "sender-network", Severity: Medium,
		Title:   "Where the email was sent from",
		Why:     "Email headers record the machines and networks a message passed through, often starting with the sender's own computer: its name, its address on the office or home network, and the public address that places it in a city or a company.",
		Fix:     "Forward the content rather than the original file, or send from a webmail account, which usually keeps your own address out of the headers.",
		Formats: []string{"eml"},
	}
	ThreadHistory = &Rule{
		ID: "SPX025", Name: "thread-history", Severity: Medium,
		Title:   "Earlier messages of the conversation",
		Why:     "Replies and forwards carry the messages before them. A forwarded email can hold a whole thread, with names, addresses and remarks that were never meant for the new recipient.",
		Fix:     "Delete the quoted history before forwarding, or copy only the part that matters into a new message.",
		Formats: []string{"eml"}, Reveal: true,
	}
	ArchiveJunk = &Rule{
		ID: "SPX023", Name: "archive-junk", Severity: Medium,
		Title:   "System files that list your folders",
		Why:     "The archive contains files the operating system creates on its own: .DS_Store and __MACOSX entries from macOS, Thumbs.db and desktop.ini from Windows. They can name files that were in the folder but are not in the archive, keep thumbnails of deleted pictures, or record where files were downloaded from.",
		Fix:     "Run `sphinx seal`, which drops them, or zip with your system's junk files excluded.",
		Formats: []string{"zip"}, Seal: true, Reveal: true,
	}
)

// Rules lists every rule in reference order.
var Rules = []*Rule{
	HiddenCrop, BlackedOutText, UnappliedRedaction, EarlierRevision, DeletedText,
	GPSLocation, ThumbnailMismatch, HiddenVideo, TrailingData, HiddenContent,
	Comments, PersonalNames, DeviceSerial, LocalPaths, EmbeddedFiles,
	EmbeddedText, Timestamps, SoftwareTrail, HiddenInstructions, InvisibleText,
	FormData, ContentCredentials, ArchiveJunk, SenderNetwork, ThreadHistory,
}

func Lookup(idOrName string) *Rule {
	for _, r := range Rules {
		if r.ID == idOrName || r.Name == idOrName {
			return r
		}
	}
	return nil
}
