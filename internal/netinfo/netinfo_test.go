package netinfo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLookupIPWhoIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"ip":"203.0.113.6","city":"Novosibirsk","region":"Novosibirsk Oblast","country":"Russia","country_code":"RU","connection":{"asn":31133,"org":"PJSC MegaFon","isp":"PJSC MegaFon"}}`))
	}))
	defer srv.Close()

	info, err := lookupIPWhoIs(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.IP != "203.0.113.6" || info.Provider != "PJSC MegaFon" || info.ASN != "AS31133" || info.City != "Novosibirsk" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestLookupIPInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ip":"203.0.113.7","city":"Novosibirsk","region":"Novosibirsk Oblast","country":"RU","org":"AS31205 PJSC MegaFon"}`))
	}))
	defer srv.Close()

	info, err := lookupIPInfo(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.IP != "203.0.113.7" || info.Provider != "PJSC MegaFon" || info.ASN != "AS31205" || info.CountryCode != "RU" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestLookupIPAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.8","city":"Moscow","region":"Moscow","country_name":"Russia","country_code":"RU","org":"Example ISP","asn":"AS64500"}`))
	}))
	defer srv.Close()

	info, err := lookupIPAPI(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.IP != "203.0.113.8" || info.Provider != "Example ISP" || info.ASN != "AS64500" || info.Country != "Russia" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestLookupIPAPICom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","query":"203.0.113.9","city":"Kazan","regionName":"Tatarstan","country":"Russia","countryCode":"RU","isp":"Example ISP","org":"Example Org","as":"AS64501 Example ISP"}`))
	}))
	defer srv.Close()

	info, err := lookupIPAPICom(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.IP != "203.0.113.9" || info.Provider != "Example ISP" || info.ASN != "AS64501" || info.City != "Kazan" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestLookupFallsBack(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.10","city":"Omsk","country":"RU","org":"AS1 Test"}`))
	}))
	defer good.Close()

	oldWho, oldInfo, oldAPI, oldCom := ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL
	defer func() { ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL = oldWho, oldInfo, oldAPI, oldCom }()
	ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL = bad.URL, bad.URL, good.URL, bad.URL

	info, err := Lookup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.IP != "203.0.113.10" || info.Source != "ipapi.co" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestLookupConsensus(t *testing.T) {
	who := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"ip":"203.0.113.20","city":"Novosibirsk","region":"Novosibirsk Oblast","country":"Russia","country_code":"RU","connection":{"asn":31133,"org":"PJSC MegaFon","isp":"PJSC MegaFon"}}`))
	}))
	defer who.Close()
	com := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","query":"203.0.113.20","city":"Novosibirsk","regionName":"Novosibirsk Oblast","country":"Russia","countryCode":"RU","isp":"PJSC MegaFon","org":"SibF mobile network","as":"AS31133 PJSC MegaFon"}`))
	}))
	defer com.Close()
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.20","city":"Kemerovo","region":"Kuzbass","country":"RU","org":"AS31133 PJSC MegaFon"}`))
	}))
	defer stale.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	oldWho, oldInfo, oldAPI, oldCom := ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL
	defer func() { ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL = oldWho, oldInfo, oldAPI, oldCom }()
	ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL = who.URL, stale.URL, bad.URL, com.URL

	info, err := Lookup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.City != "Novosibirsk" || info.Region != "Novosibirsk Oblast" {
		t.Fatalf("consensus picked %q/%q: %+v", info.City, info.Region, info)
	}
	if info.Source != "ipwho.is" {
		t.Fatalf("source = %q, want ipwho.is", info.Source)
	}
	if info.IP != "203.0.113.20" || info.ASN != "AS31133" {
		t.Fatalf("unexpected merged info: %+v", info)
	}
}

func TestLookupAllFail(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	oldWho, oldInfo, oldAPI, oldCom := ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL
	defer func() { ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL = oldWho, oldInfo, oldAPI, oldCom }()
	ipwhoURL, ipinfoURL, ipapiURL, ipapiComURL = bad.URL, bad.URL, bad.URL, bad.URL

	_, err := Lookup(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ipwho.is") || !strings.Contains(err.Error(), "ipinfo.io") {
		t.Fatalf("expected combined error, got %v", err)
	}
}
