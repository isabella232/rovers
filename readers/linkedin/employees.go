package linkedin

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/src-d/rovers/client"
	"gop.kg/src-d/domain@v2.1/models/company"

	"github.com/PuerkitoBio/goquery"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	UserAgent                  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.10; rv:41.0) Gecko/20100101 Firefox/41.0"
	CookieFixtureEiso          = `lang="v=2&lang=en-us"; JSESSIONID="ajax:1520667544772704592"; bcookie="v=2&f44aa07b-441e-412b-8c8a-80655b2c28ac"; bscookie="v=1&20151203200700c4f05e9d-a567-44b4-8494-c6ac0a8fa25bAQEinMH9Jy50LwIOF-88rTAPgbWBfCOP"; lidc="b=TB30:g=293:u=299:i=1449173231:t=1449257742:s=AQGUoS6johJ9m-WI5hEEkvEfAVnSgodo"; visit="v=1&M"; sl="v=1&06kn0"; li_at=AQEDAQB8ujIBqRTgAAABUWl0pbQAAAFRaeKCtE4Ae6xI71A-EqX1D9aoQC_4zogNfQzbiFmPuj1p3789dBSqKurHn8pZYSY3xsdGwHg7HCNv3asAutapmknIl7QwNoUQBGVhPGTwwYu2IWSc8ofIPOwM; liap=true; RT=s=1449173231804&r=https%3A%2F%2Fwww.linkedin.com%2F; _lipt=0_drjeMq_FIWa51KcAdLl778KV2ofmpJN4ubJnSQhhfxryLg-QZEnYahfJN0gj37uIzr4nX1dtjcUGoaAhjYdGpr5cBLfEGdhuQ_QzXcxhCtF9oBY4VnsrN4SHIw6uxWAhSGUQ6rRbayS9QAoniDwNTqYuPGMeHG8pMVrC-az8E0WUsiazkg2chBzECIhCLSHmqkwhFzftewFu0vb2eu-Cak8ARWFIhWmY58ohfplGGG9wePNZj8t4GuK2JILfGPH85mFZELdPhsZO5s2qqt1LscrB1xVuZ9fQUTiwoZuy74OpluRyrIa4qjjL2p0hZpn8Sxv7ZQ5Eqri6dh4aWRvSMepYnY3QzqsrLcHJphtSjia6eQtIt313hKiRyrB3sMn_S_OCBSbwDtc1Vwi3NncuhvlJCd7weCShFpjj1NEJGs8; L1c=1c8a7721; oz_props_fetch_size1_8174130=15; wutan=HvWGzeZMiaQQIQAHIYpw53iIXcKKQ0CeQv4zmEZsaxU=; share_setting=PUBLIC; sdsc=1%3A1SZM1shxDNbLt36wZwCgPgvN58iw%3D`
	BaseURL                    = "https://www.linkedin.com"
	EmployeesURL               = BaseURL + "/vsearch/p?f_CC=%d"
	LinkedInEmployeesRateLimit = 5 * time.Second
)

type LinkedInWebCrawler struct {
	client *client.Client
	cookie string
}

func NewLinkedInWebCrawler(client *client.Client, cookie string) *LinkedInWebCrawler {
	return &LinkedInWebCrawler{client: client, cookie: cookie}
}

func (li *LinkedInWebCrawler) GetEmployees(companyId int) (
	people []company.Employee, err error,
) {
	start := time.Now()
	url := fmt.Sprintf(EmployeesURL, companyId)

	for {
		var more []Person
		log15.Info("Processing", "url", url)
		url, more, err = li.doGetEmployes(url)

		for _, person := range more {
			people = append(people, person.ToDomainCompanyEmployee())
		}

		if err != nil || url == "" {
			break
		}
	}

	log15.Info("Done",
		"elapsed", time.Since(start),
		"found", len(people),
	)
	// for idx, person := range people {
	// 	log15.Debug("Person", "idx", idx, "person", person)
	// }
	return people, err
}

func (li *LinkedInWebCrawler) doGetEmployes(url string) (
	next string, people []Person, err error,
) {
	start := time.Now()
	defer func() {
		needsWait := LinkedInEmployeesRateLimit - time.Since(start)
		if needsWait > 0 {
			log15.Debug("Waiting", "duration", needsWait)
			time.Sleep(needsWait)
		}
	}()
	req, err := client.NewRequest(url)
	if err != nil {
		return
	}
	req.Header.Add("User-Agent", UserAgent)
	req.Header.Add("Cookie", li.cookie)

	doc, res, err := li.client.DoHTML(req)
	if err != nil {
		return
	}
	log15.Debug("DoHTML", "url", req.URL, "status", res.StatusCode)
	if res.StatusCode == 404 {
		err = client.NotFound
		return
	}
	return li.parseContent(doc)
}

func (li *LinkedInWebCrawler) parseContent(doc *goquery.Document) (
	next string, people []Person, err error,
) {
	content, err := doc.Find("#voltron_srp_main-content").Html()
	if err != nil {
		return
	}

	// Fix encoding issues with LinkedIn's JSON:
	// Source: http://stackoverflow.com/q/30270668
	content = strings.Replace(content, "\\u002d", "-", -1)

	length := len(content)
	jsonBlob := content[4 : length-3]

	var data LinkedInData
	err = json.Unmarshal([]byte(jsonBlob), &data)
	if err != nil {
		return
	}

	next = data.getNextURL()
	people = data.getPeople()
	return
}

// fat ass LinkedIn format
type LinkedInData struct {
	Content struct {
		Page struct {
			V struct {
				Search struct {
					Data struct {
						Pagination struct {
							Pages []struct {
								Current bool   `json:"isCurrentPage"`
								URL     string `json:"pageURL"`
							}
						} `json:"resultPagination"`
					} `json:"baseData"`
					Results []struct {
						Person Person
					}
				}
			} `json:"voltron_unified_search_json"`
		}
	}
}

func (lid *LinkedInData) getNextURL() string {
	next := false
	for _, page := range lid.Content.Page.V.Search.Data.Pagination.Pages {
		if page.Current {
			next = true
			continue
		}

		if next {
			return BaseURL + page.URL
		}
	}

	return ""
}

func (lid *LinkedInData) getPeople() []Person {
	var people []Person
	for _, result := range lid.Content.Page.V.Search.Results {
		people = append(people, result.Person)
	}
	return people
}

type Person struct {
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	LinkedInId int    `json:"id"`
	Location   string `json:"fmt_location"`
	Position   string `json:"fmt_headline"`
}

func (p *Person) ToDomainCompanyEmployee() company.Employee {
	return company.Employee{
		FirstName:  p.FirstName,
		LastName:   p.LastName,
		LinkedInId: p.LinkedInId,
		Location:   p.Location,
		Position:   p.Position,
	}
}
