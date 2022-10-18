package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

type BigPandaClient struct {
	Client *http.Client
	URL    string
	Token  string
	AppKey string
}

type BigPandaAlerts struct {
	AppKey string `json:"app_key"`
	// TODO : Use static values for critical, warning, info, OK ; use validator or interface ?!?
	// TODO : Try to replace Alerts with []BigPandaAlerts and remove BigPandaAlert struct
	Status             string          `json:"status"`
	Host               string          `json:"host,omitempty"`
	Timestamp          int64           `json:"timestamp,omitempty"`
	Check              string          `json:"check,omitempty"`
	Description        string          `json:"description,omitempty"`
	Cluster            string          `json:"cluster,omitempty"`
	IncidentIdentifier string          `json:"incident_identifier,omitempty"`
	Alerts             []BigPandaAlert `json:"alerts,omitempty"`
}

type BigPandaAlert struct {
	Status             string `json:"status"`
	Host               string `json:"host,omitempty"`
	Timestamp          int64  `json:"timestamp,omitempty"`
	Check              string `json:"check,omitempty"`
	Description        string `json:"description,omitempty"`
	Cluster            string `json:"cluster,omitempty"`
	IncidentIdentifier string `json:"incident_identifier,omitempty"`
}

type CiqHealthEvent struct {
	SystemDisplayIdentifier string     `json:"system_display_identifier,omitempty"`
	SystemName              string     `json:"system_name"`
	SystemModel             string     `json:"system_model,omitempty"`
	Timestamp               int64      `json:"timestamp"`
	TimestampIso8601        string     `json:"timestamp_iso8601,omitempty"`
	CurrentScore            int        `json:"current_score,omitempty"`
	NewIssues               []CiqIssue `json:"new_issues,omitempty"`
	ResolvedIssues          []CiqIssue `json:"resolved_issues,omitempty"`
}

type CiqIssue struct {
	ID              string `json:"id,omitempty"`
	Impact          int    `json:"impact,omitempty"`
	Description     string `json:"description,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
	RuleID          string `json:"rule_id,omitempty"`
	Category        string `json:"category,omitempty"`
	ImpactedObjects []struct {
		ObjectNativeID   string      `json:"object_native_id,omitempty"`
		ObjectName       interface{} `json:"object_name,omitempty"`
		ObjectID         string      `json:"object_id,omitempty"`
		ObjectNativeType string      `json:"object_native_type,omitempty"`
	} `json:"impacted_objects,omitempty"`
}

var bpClient BigPandaClient

func init() {
	functions.HTTP("CiqEventToBigPandaAlert", CiqEventToBigPandaAlert)

	url := os.Getenv("BP_URL")
	if len(url) == 0 {
		// url = "https://api.bigpanda.io/data/v2/alerts"
		url = "https://api.bigpanda.io/data/v2/alerts"
	}

	token := os.Getenv("BP_TOKEN")
	if len(token) == 0 {
		log.Fatalf("missing BigPanda token")
	}

	app_key := os.Getenv("BP_APP_KEY")
	if len(app_key) == 0 {
		log.Fatalf("missing BigPanda Application Key")
	}

	bpClient = BigPandaClient{
		Client: &http.Client{
			Timeout: time.Second * 30,
		},
		URL:    url,
		Token:  token,
		AppKey: app_key,
	}
}

func setHeader(r *http.Request, bp *BigPandaClient) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Add("Authorization", "Bearer "+bp.Token)
}

//// TODO implement signature check
// func Verify(msg, key []byte, hash string) (bool, error) {
// 	sig, err := hex.DecodeString(hash)
// 	if err != nil {
// 		return false, err
// 	}

// 	mac := hmac.New(sha256.New, key)
// 	mac.Write(msg)

// 	return hmac.Equal(sig, mac.Sum(nil)), nil
// }

// BigPanda status values: ok,ok-suspect,warning,warning-suspect,critical,critical-suspect,unknown,acknowledged,oksuspect,warningsuspect,criticalsuspect,ok_suspect,warning_suspect,critical_suspect,ok suspect,warning suspect,critical suspect
func statusForScore(s int) string {
	if s == 100 {
		return "ok"
	} else if s <= 99 && s > 75 {
		return "ok suspect"
	} else if s <= 75 && s > 70 {
		return "warning"
	} else if s <= 70 {
		return "critical"
	} else {
		return "unknown"
	}
}

// TODO return an error if wrong parsing
// TODO replace *BigPandaClient by a context ?
func CiqEventMapping(c *CiqHealthEvent, bp *BigPandaClient) *BigPandaAlerts {
	fmt.Println("mapping input CloudIQ event: ")
	fmt.Printf("%+v", c)
	alert := BigPandaAlerts{
		AppKey:  bp.AppKey,
		Cluster: "CloudIQ",
		Host:    c.SystemName,
	}

	if len(c.NewIssues) > 0 {
		for _, v := range c.NewIssues {
			alert.Alerts = append(alert.Alerts, BigPandaAlert{
				Status:             statusForScore(c.CurrentScore),
				Timestamp:          c.Timestamp,
				Host:               c.SystemName,
				Description:        v.Description,
				Check:              v.RuleID,
				IncidentIdentifier: v.ID,
			})
		}
	}
	if len(c.ResolvedIssues) > 0 {
		// Send Status OK is equivalent to resolve Alert (https://docs.bigpanda.io/reference/resolve-alerts)
		for _, v := range c.ResolvedIssues {
			alert.Alerts = append(alert.Alerts, BigPandaAlert{
				Status:             "ok",
				Timestamp:          c.Timestamp,
				Host:               c.SystemName,
				Description:        v.Description,
				Check:              v.RuleID,
				IncidentIdentifier: v.ID,
			})
		}
	}
	fmt.Println("mapping output BigPanda Alert: ")
	fmt.Printf("%+v\n", alert)
	return &alert
}

// Function triggered by GCP Cloud Function
func CiqEventToBigPandaAlert(w http.ResponseWriter, r *http.Request) {
	fmt.Println("call CiqEventToBigPandaAlert")
	fmt.Println("------")
	reqDump, _ := httputil.DumpRequest(r, true)

	fmt.Println(string(reqDump))

	var ciqEvent CiqHealthEvent

	switch r.Header.Get("X-ciq-event") {
	case "ping":
		fmt.Fprint(w, "pong!")
		return
	case "health-issue-change":
		// TODO need log level
		fmt.Println("health issue --- json decoding")
		fmt.Println(r.Body)

		// CloudIQ webhook POST with health score
		if err := json.NewDecoder(r.Body).Decode(&ciqEvent); err != nil {
			fmt.Printf("json.NewDecoder Payload err: %v", err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusUnprocessableEntity)
			return
		}

		// Convert
		alert := CiqEventMapping(&ciqEvent, &bpClient)
		fmt.Println("Event Mapping passed!!!")
		fmt.Print(alert)

		// Send to BigPanda
		payloadBuf := new(bytes.Buffer)
		json.NewEncoder(payloadBuf).Encode(alert)
		fmt.Println(payloadBuf)

		fmt.Println("Craft Post Request")
		req, err := http.NewRequest("POST", bpClient.URL, payloadBuf)
		if err != nil {
			fmt.Println("Error NewRequest")
			reqDump, _ := httputil.DumpRequest(req, true)
			fmt.Println(string(reqDump))
			fmt.Print(err)
			return
		}

		fmt.Println("Trigger Post BigPanda")
		setHeader(req, &bpClient)
		res, err := bpClient.Client.Do(req)
		if err != nil {
			fmt.Println("Error Post to BigPanda")
			resDump, err := httputil.DumpResponse(res, true)
			fmt.Println(string(resDump))
			fmt.Print(err)
			return
		}
		fmt.Println("Dump Response BigPanda")
		fmt.Println(res)
		resDump, err := httputil.DumpResponse(res, true)
		if err != nil {
			fmt.Print(err)
			return
		}

		fmt.Println(string(resDump))
		return

	default:
		fmt.Println("unknown or missing x-ciq-event")
		http.Error(w, "unknown or missing x-ciq-event", http.StatusNotAcceptable)
		return
	}
}
