package benchmark_test

import (
	"fmt"
	"strings"
)

// firstNames / lastNames / orgs / cities are sampled by index to produce
// varied but deterministic NER entities across the generated corpus.
var firstNames = []string{
	"Alice", "Bob", "Carol", "David", "Emma", "Frank", "Grace", "Henry",
	"Isabel", "James", "Karen", "Liam", "Maria", "Noah", "Olivia", "Paul",
	"Quinn", "Rachel", "Samuel", "Tina", "Uma", "Victor", "Wendy", "Xavier",
}
var lastNames = []string{
	"Johnson", "Smith", "Williams", "Brown", "Jones", "Garcia", "Miller",
	"Davis", "Wilson", "Moore", "Taylor", "Anderson", "Thomas", "Jackson",
	"White", "Harris", "Martin", "Thompson", "Lee", "Walker", "Hall", "Allen",
}
var orgs = []string{
	"Microsoft", "Google", "Amazon", "Apple", "Goldman Sachs", "JPMorgan",
	"IBM", "Oracle", "Salesforce", "Cisco", "Intel", "Nvidia", "Meta",
	"Netflix", "Adobe", "Spotify", "Uber", "Airbnb", "Stripe", "Twilio",
}
var cities = []string{
	"Seattle", "New York", "San Francisco", "Chicago", "Boston", "Austin",
	"London", "Berlin", "Paris", "Tokyo", "Sydney", "Toronto", "Amsterdam",
	"Dublin", "Singapore", "Zurich", "Stockholm", "Copenhagen", "Oslo",
}

// nerTemplates produce sentences with PERSON, ORGANIZATION, and LOCATION entities.
var nerTemplates = []string{
	"%s %s, engineer at %s, works remotely from %s.",
	"%s %s joined %s as a consultant based in %s.",
	"The %s office in %s hired %s %s last quarter.",
	"%s %s from %s presented at the %s conference.",
	"According to %s %s, %s expanded operations into %s.",
}

// patternFragments produce regex-detectable PII snippets.
var patternFragments = []string{
	"email %s%d@example.com",
	"phone +1-800-555-%04d",
	"SSN 5%02d-4%d-678%d",
	"credit card 4111111111111%03d",
	"server 192.168.%d.%d",
	"visit https://service%d.example.com/account",
	"IBAN GB%02dNWBK6016133192%04d",
	"wallet 1BvBMSEYstWetqTFn5Au4m4GFg7xJa%04d",
	"born on 19%02d-0%d-1%d",
	"mac 00:1A:2B:3C:%02X:%02X",
}

// filler is neutral prose with no detectable PII or named entities.
var filler = []string{
	"The system processes requests asynchronously using an event-driven architecture.",
	"All financial transactions must be reviewed by the compliance department before approval.",
	"Our cloud infrastructure is deployed across multiple availability zones for redundancy.",
	"The support team processed over ten thousand tickets during the quarter with high satisfaction.",
	"Data retention policies require secure deletion of records older than seven years.",
	"The engineering team completed the migration to the new distributed storage system.",
	"Regulatory requirements mandate that all personal data be encrypted at rest and in transit.",
	"The quarterly audit revealed no significant deviations from the established security protocols.",
	"Incident response procedures were updated to reflect the latest threat intelligence findings.",
	"Customer feedback indicated a strong preference for self-service portal functionality.",
}

func nerSentence(i int) string {
	fn := firstNames[i%len(firstNames)]
	ln := lastNames[(i+3)%len(lastNames)]
	org := orgs[i%len(orgs)]
	city := cities[(i+5)%len(cities)]
	tpl := nerTemplates[i%len(nerTemplates)]
	switch i % len(nerTemplates) {
	case 0:
		return fmt.Sprintf(tpl, fn, ln, org, city)
	case 1:
		return fmt.Sprintf(tpl, fn, ln, org, city)
	case 2:
		return fmt.Sprintf(tpl, org, city, fn, ln)
	case 3:
		return fmt.Sprintf(tpl, fn, ln, org, city)
	default:
		return fmt.Sprintf(tpl, fn, ln, org, city)
	}
}

func patternFragment(i int) string {
	n := i % len(patternFragments)
	switch n {
	case 0:
		fn := strings.ToLower(firstNames[i%len(firstNames)])
		return fmt.Sprintf(patternFragments[n], fn, i%1000)
	case 1:
		return fmt.Sprintf(patternFragments[n], i%10000)
	case 2:
		return fmt.Sprintf(patternFragments[n], i%100, i%10, i%10)
	case 3:
		return fmt.Sprintf(patternFragments[n], i%1000)
	case 4:
		return fmt.Sprintf(patternFragments[n], i%256, (i+1)%256)
	case 5:
		return fmt.Sprintf(patternFragments[n], i%1000)
	case 6:
		return fmt.Sprintf(patternFragments[n], 29+i%70, i%10000)
	case 7:
		return fmt.Sprintf(patternFragments[n], i%10000)
	case 8:
		return fmt.Sprintf(patternFragments[n], 60+i%40, 1+i%9, i%9)
	default:
		return fmt.Sprintf(patternFragments[n], i%256, (i+7)%256)
	}
}

// GenerateText returns a document of ~targetBytes bytes with both pattern PII
// and NER entities (PERSON, ORGANIZATION, LOCATION) at realistic density.
// Every ~nerInterval chars a NER sentence is injected;
// every ~patternInterval chars a pattern PII fragment is injected.
func GenerateText(targetBytes, nerInterval int) string {
	patternInterval := nerInterval + 150
	var b strings.Builder
	b.Grow(targetBytes + 512)

	fillerIdx, nerIdx, patternIdx, written := 0, 0, 0, 0

	for written < targetBytes {
		chunk := filler[fillerIdx%len(filler)] + "\n"
		b.WriteString(chunk)
		written += len(chunk)
		fillerIdx++

		if written%nerInterval < len(chunk) {
			line := nerSentence(nerIdx) + "\n"
			b.WriteString(line)
			written += len(line)
			nerIdx++
		}

		if written%patternInterval < len(chunk) {
			line := "Record: " + patternFragment(patternIdx) + ".\n"
			b.WriteString(line)
			written += len(line)
			patternIdx++
		}
	}
	return b.String()
}

// GenerateBatch returns n independent documents each of ~docBytes bytes.
func GenerateBatch(n, docBytes int) []string {
	docs := make([]string, n)
	for i := range docs {
		docs[i] = GenerateText(docBytes, 200+i%300)
	}
	return docs
}
