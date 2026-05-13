// Command gen_eval emits a labeled JSONL eval corpus by composing each
// document from prefix + entity + suffix segments. Positions are computed
// from segment lengths so they're guaranteed correct.
//
//go:build ignore

package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
)

type entitySpan struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Type  string `json:"type"`
}

type doc struct {
	ID       string       `json:"id"`
	Text     string       `json:"text"`
	Entities []entitySpan `json:"entities"`
}

// segment is one concatenation unit. If Type is non-empty it's a labeled
// entity; otherwise it's just literal filler text.
type segment struct {
	Text string
	Type string
}

func makeDoc(id string, segs ...segment) doc {
	var (
		buf      []byte
		entities []entitySpan
	)
	for _, s := range segs {
		start := len(buf)
		buf = append(buf, s.Text...)
		end := len(buf)
		if s.Type != "" {
			entities = append(entities, entitySpan{Start: start, End: end, Type: s.Type})
		}
	}
	return doc{ID: id, Text: string(buf), Entities: entities}
}

func plain(s string) segment            { return segment{Text: s} }
func ent(s, t string) segment           { return segment{Text: s, Type: t} }

func corpus() []doc {
	out := []doc{
		// --- People / orgs / locations (NER core) -------------------------
		makeDoc("ner-1",
			plain("Hi, I'm "),
			ent("John Smith", "PERSON"),
			plain(" from "),
			ent("Acme Corp", "ORGANIZATION"),
			plain(" in "),
			ent("New York", "LOCATION"),
			plain("."),
		),
		makeDoc("ner-2",
			ent("Alice Johnson", "PERSON"),
			plain(" works at "),
			ent("Microsoft", "ORGANIZATION"),
			plain(" in "),
			ent("Seattle", "LOCATION"),
			plain("."),
		),
		makeDoc("ner-3",
			plain("Please cc "),
			ent("Maria Garcia", "PERSON"),
			plain(" on the meeting with "),
			ent("Goldman Sachs", "ORGANIZATION"),
			plain("."),
		),
		makeDoc("ner-4",
			ent("Sarah Williams", "PERSON"),
			plain(" relocated from "),
			ent("Boston", "LOCATION"),
			plain(" to "),
			ent("San Francisco", "LOCATION"),
			plain(" last quarter."),
		),
		makeDoc("ner-5",
			plain("I spoke with "),
			ent("David Lee", "PERSON"),
			plain(" at "),
			ent("Apple", "ORGANIZATION"),
			plain(" about the "),
			ent("Cupertino", "LOCATION"),
			plain(" office."),
		),
		makeDoc("ner-6",
			ent("Robert Brown", "PERSON"),
			plain(" of "),
			ent("Tesla", "ORGANIZATION"),
			plain(" presented at the conference."),
		),
		makeDoc("ner-7",
			plain("Forwarded from "),
			ent("Emily Davis", "PERSON"),
			plain(" — please review."),
		),

		// --- Email --------------------------------------------------------
		makeDoc("email-1",
			plain("Reach me at "),
			ent("alice@example.com", "EMAIL_ADDRESS"),
			plain(" for details."),
		),
		makeDoc("email-2",
			plain("Send the report to "),
			ent("bob.smith@company.co.uk", "EMAIL_ADDRESS"),
			plain(" and "),
			ent("carol@example.org", "EMAIL_ADDRESS"),
			plain("."),
		),
		makeDoc("email-3",
			plain("From: "),
			ent("noreply@service.io", "EMAIL_ADDRESS"),
			plain("\nReply-To: "),
			ent("support@service.io", "EMAIL_ADDRESS"),
		),
		makeDoc("email-4",
			plain("Customer email "),
			ent("john.doe+filter@gmail.com", "EMAIL_ADDRESS"),
			plain(" needs verification."),
		),
		makeDoc("email-5",
			plain("Contact: "),
			ent("hr@startup.dev", "EMAIL_ADDRESS"),
		),

		// --- Phone --------------------------------------------------------
		makeDoc("phone-1",
			plain("Call me at "),
			ent("+1-800-555-0199", "PHONE_NUMBER"),
			plain(" today."),
		),
		makeDoc("phone-2",
			plain("Office phone: "),
			ent("(415) 555-2671", "PHONE_NUMBER"),
		),
		makeDoc("phone-3",
			plain("UK office "),
			ent("+44 20 7946 0958", "PHONE_NUMBER"),
		),
		makeDoc("phone-4",
			plain("Tel "),
			ent("212-555-0143", "PHONE_NUMBER"),
			plain(" if urgent."),
		),

		// --- US SSN -------------------------------------------------------
		makeDoc("ssn-1",
			plain("SSN: "),
			ent("123-45-6789", "US_SSN"),
		),
		makeDoc("ssn-2",
			plain("My social security number is "),
			ent("078-05-1120", "US_SSN"),
			plain("."),
		),
		makeDoc("ssn-3",
			plain("Tax form lists "),
			ent("219-09-9999", "US_SSN"),
			plain(" on file."),
		),

		// --- IP -----------------------------------------------------------
		makeDoc("ip-1",
			plain("Server "),
			ent("10.0.0.5", "IP_ADDRESS"),
			plain(" returned 500."),
		),
		makeDoc("ip-2",
			plain("Client IP "),
			ent("192.168.1.100", "IP_ADDRESS"),
			plain(" — bad request."),
		),
		makeDoc("ip-3",
			plain("Connection from "),
			ent("203.0.113.42", "IP_ADDRESS"),
			plain(" denied."),
		),
		makeDoc("ip-4",
			plain("[error] remote "),
			ent("198.51.100.7", "IP_ADDRESS"),
			plain(" timeout"),
		),

		// --- Credit card --------------------------------------------------
		makeDoc("cc-1",
			plain("Card "),
			ent("4111-1111-1111-1111", "CREDIT_CARD"),
			plain(" was declined."),
		),
		makeDoc("cc-2",
			plain("Charge to "),
			ent("5555 5555 5555 4444", "CREDIT_CARD"),
		),
		makeDoc("cc-3",
			plain("Visa "),
			ent("4012888888881881", "CREDIT_CARD"),
			plain(" expires 12/26."),
		),

		// --- UK NHS / NINO ------------------------------------------------
		makeDoc("uk-1",
			plain("Patient NHS "),
			ent("401 023 2137", "UK_NHS"),
			plain(" admitted."),
		),
		makeDoc("uk-2",
			plain("National insurance number "),
			ent("AB123456C", "UK_NINO"),
			plain(" on record."),
		),

		// --- IT fiscal code / VAT ----------------------------------------
		makeDoc("it-1",
			plain("Codice fiscale: "),
			ent("RSSMRA85T10A562S", "IT_FISCAL_CODE"),
		),
		makeDoc("it-2",
			plain("VAT "),
			ent("IT 12345670785", "IT_VAT_CODE"),
			plain(" registered."),
		),

		// --- ES NIF / NIE -------------------------------------------------
		makeDoc("es-1",
			plain("DNI "),
			ent("12345678Z", "ES_NIF"),
			plain(" verificado."),
		),
		makeDoc("es-2",
			plain("NIE "),
			ent("X0000000T", "ES_NIE"),
			plain(" residency confirmed."),
		),

		// --- IN PAN / Aadhaar --------------------------------------------
		makeDoc("in-1",
			plain("PAN "),
			ent("ABCDE1234F", "IN_PAN"),
			plain(" filed for tax review."),
		),

		// --- AU TFN / ABN -------------------------------------------------
		makeDoc("au-1",
			plain("TFN "),
			ent("123 456 782", "AU_TFN"),
			plain(" lodged."),
		),
		makeDoc("au-2",
			plain("ABN "),
			ent("51 824 753 556", "AU_ABN"),
			plain(" invoice."),
		),

		// --- PL PESEL -----------------------------------------------------
		makeDoc("pl-1",
			plain("PESEL "),
			ent("02070803628", "PL_PESEL"),
			plain(" obywatel."),
		),

		// --- Mixed / log-line style ---------------------------------------
		makeDoc("mix-1",
			plain("[ERROR] user "),
			ent("Alice Johnson", "PERSON"),
			plain(" (email "),
			ent("alice@example.com", "EMAIL_ADDRESS"),
			plain(") from IP "),
			ent("10.0.0.5", "IP_ADDRESS"),
			plain(" failed login."),
		),
		makeDoc("mix-2",
			plain("Customer "),
			ent("John Smith", "PERSON"),
			plain(" of "),
			ent("Acme Corp", "ORGANIZATION"),
			plain(" charged "),
			ent("4111-1111-1111-1111", "CREDIT_CARD"),
			plain(" — phone "),
			ent("+1-800-555-0199", "PHONE_NUMBER"),
			plain("."),
		),
		makeDoc("mix-3",
			plain("Patient "),
			ent("Maria Garcia", "PERSON"),
			plain(" NHS "),
			ent("401 023 2137", "UK_NHS"),
			plain(" SSN "),
			ent("123-45-6789", "US_SSN"),
			plain(" admitted."),
		),
		makeDoc("mix-4",
			plain("Wire transfer from "),
			ent("Robert Brown", "PERSON"),
			plain(" — IBAN GB29 NWBK 6016 1331 9268 19, contact "),
			ent("bob@example.com", "EMAIL_ADDRESS"),
			plain("."),
		),
		makeDoc("mix-5",
			plain("From "),
			ent("Emily Davis", "PERSON"),
			plain(" at "),
			ent("Microsoft", "ORGANIZATION"),
			plain(": meet at "),
			ent("Seattle", "LOCATION"),
			plain(" office."),
		),

		// --- Negative / no PII --------------------------------------------
		makeDoc("neg-1",
			plain("The system reported a routine status update with no incidents to log."),
		),
		makeDoc("neg-2",
			plain("Quarterly performance metrics improved across all dimensions this period."),
		),
		makeDoc("neg-3",
			plain("Please review the attached invoice template before next Tuesday."),
		),
	}
	return out
}

func main() {
	out := flag.String("out", "", "output jsonl path")
	flag.Parse()
	if *out == "" {
		log.Fatal("--out required")
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, d := range corpus() {
		if err := enc.Encode(d); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("wrote %d docs to %s", len(corpus()), *out)
}
