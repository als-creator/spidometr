// Package netinfo определяет публичный IP, провайдера и геопозицию.
package netinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Info — сведения о текущем подключении.
type Info struct {
	IP          string
	Provider    string
	ASN         string
	Org         string
	City        string
	Region      string
	Country     string
	CountryCode string
	Source      string
}

// Адреса вынесены в переменные, чтобы тесты могли подставить httptest-сервер.
var (
	ipwhoURL    = "https://ipwho.is/"
	ipinfoURL   = "https://ipinfo.io/json"
	ipapiURL    = "https://ipapi.co/json/"
	ipapiComURL = "http://ip-api.com/json/?fields=status,message,country,countryCode,regionName,city,isp,org,as,query"
)

var client = &http.Client{Timeout: 6 * time.Second}

type provider struct {
	name string
	url  string
	load func(context.Context, string) (Info, error)
}

type providerResult struct {
	name string
	info Info
	err  error
}

// Lookup опрашивает несколько геолокаторов и выбирает город/регион по
// большинству совпадений. Первый в списке источник выигрывает при равенстве:
// ipwho.is и ip-api.com заметно свежее ipinfo.io для российских сетей.
func Lookup(ctx context.Context) (Info, error) {
	providers := []provider{
		{"ipwho.is", ipwhoURL, lookupIPWhoIs},
		{"ip-api.com", ipapiComURL, lookupIPAPICom},
		{"ipinfo.io", ipinfoURL, lookupIPInfo},
		{"ipapi.co", ipapiURL, lookupIPAPI},
	}

	results := make([]providerResult, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func(i int, p provider) {
			defer wg.Done()
			info, err := p.load(ctx, p.url)
			results[i] = providerResult{name: p.name, info: info, err: err}
		}(i, p)
	}
	wg.Wait()

	var (
		ok   []providerResult
		errs []error
	)
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.name, r.err))
			continue
		}
		ok = append(ok, r)
	}
	if len(ok) == 0 {
		return Info{}, errors.Join(errs...)
	}

	best := ok[0]
	bestScore := placeScore(best.info, ok)
	for _, r := range ok[1:] {
		if score := placeScore(r.info, ok); score > bestScore {
			best, bestScore = r, score
		}
	}

	info := best.info
	info.Source = best.name
	for _, r := range ok {
		fillMissing(&info, r.info)
	}
	return info, nil
}

// placeScore считает, насколько город и регион результата совпадают с
// остальными ответами. Пустые значения не участвуют в голосовании.
func placeScore(info Info, results []providerResult) int {
	score := 0
	city := normalizePlace(info.City)
	region := normalizePlace(info.Region)
	for _, r := range results {
		if city != "" && city == normalizePlace(r.info.City) {
			score++
		}
		if region != "" && region == normalizePlace(r.info.Region) {
			score++
		}
	}
	return score
}

func normalizePlace(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func fillMissing(dst *Info, src Info) {
	if dst.IP == "" {
		dst.IP = src.IP
	}
	if dst.Provider == "" {
		dst.Provider = src.Provider
	}
	if dst.ASN == "" {
		dst.ASN = src.ASN
	}
	if dst.Org == "" {
		dst.Org = src.Org
	}
	if dst.City == "" {
		dst.City = src.City
	}
	if dst.Region == "" {
		dst.Region = src.Region
	}
	if dst.Country == "" {
		dst.Country = src.Country
	}
	if dst.CountryCode == "" {
		dst.CountryCode = src.CountryCode
	}
}

func getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "github.com/als-creator/spidometr/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func lookupIPWhoIs(ctx context.Context, url string) (Info, error) {
	var v struct {
		Success     bool   `json:"success"`
		Message     string `json:"message"`
		IP          string `json:"ip"`
		City        string `json:"city"`
		Region      string `json:"region"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		Connection  struct {
			ASN int    `json:"asn"`
			Org string `json:"org"`
			ISP string `json:"isp"`
		} `json:"connection"`
	}
	if err := getJSON(ctx, url, &v); err != nil {
		return Info{}, err
	}
	if !v.Success {
		return Info{}, fmt.Errorf("status error: %s", v.Message)
	}
	info := Info{
		IP:          v.IP,
		Provider:    v.Connection.ISP,
		Org:         v.Connection.Org,
		City:        v.City,
		Region:      v.Region,
		Country:     v.Country,
		CountryCode: v.CountryCode,
	}
	if info.Provider == "" {
		info.Provider = v.Connection.Org
	}
	if v.Connection.ASN > 0 {
		info.ASN = fmt.Sprintf("AS%d", v.Connection.ASN)
	}
	if info.IP == "" {
		return Info{}, errors.New("empty IP")
	}
	return info, nil
}

func lookupIPInfo(ctx context.Context, url string) (Info, error) {
	var v struct {
		IP      string `json:"ip"`
		City    string `json:"city"`
		Region  string `json:"region"`
		Country string `json:"country"`
		Org     string `json:"org"`
	}
	if err := getJSON(ctx, url, &v); err != nil {
		return Info{}, err
	}
	info := Info{
		IP:          v.IP,
		City:        v.City,
		Region:      v.Region,
		CountryCode: v.Country,
		Org:         v.Org,
	}
	info.ASN, info.Provider = splitOrg(v.Org)
	if info.IP == "" {
		return Info{}, errors.New("empty IP")
	}
	return info, nil
}

func lookupIPAPI(ctx context.Context, url string) (Info, error) {
	var v struct {
		IP          string `json:"ip"`
		City        string `json:"city"`
		Region      string `json:"region"`
		Country     string `json:"country_name"`
		CountryCode string `json:"country_code"`
		Org         string `json:"org"`
		ASN         string `json:"asn"`
	}
	if err := getJSON(ctx, url, &v); err != nil {
		return Info{}, err
	}
	info := Info{
		IP:          v.IP,
		Provider:    v.Org,
		ASN:         v.ASN,
		Org:         v.Org,
		City:        v.City,
		Region:      v.Region,
		Country:     v.Country,
		CountryCode: v.CountryCode,
	}
	if info.IP == "" {
		return Info{}, errors.New("empty IP")
	}
	return info, nil
}

func lookupIPAPICom(ctx context.Context, url string) (Info, error) {
	var v struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Query       string `json:"query"`
		City        string `json:"city"`
		RegionName  string `json:"regionName"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		ISP         string `json:"isp"`
		Org         string `json:"org"`
		AS          string `json:"as"`
	}
	if err := getJSON(ctx, url, &v); err != nil {
		return Info{}, err
	}
	if v.Status != "" && v.Status != "success" {
		return Info{}, fmt.Errorf("status %s: %s", v.Status, v.Message)
	}
	info := Info{
		IP:          v.Query,
		Provider:    v.ISP,
		Org:         v.Org,
		City:        v.City,
		Region:      v.RegionName,
		Country:     v.Country,
		CountryCode: v.CountryCode,
	}
	info.ASN, _ = splitOrg(v.AS)
	if info.Provider == "" {
		info.Provider = v.Org
	}
	if info.IP == "" {
		return Info{}, errors.New("empty IP")
	}
	return info, nil
}

// splitOrg отделяет ASN от названия провайдера: "AS31205 PJSC MegaFon".
func splitOrg(org string) (asn, provider string) {
	fields := strings.Fields(org)
	if len(fields) > 0 && strings.HasPrefix(strings.ToUpper(fields[0]), "AS") {
		asn = fields[0]
		provider = strings.TrimSpace(strings.TrimPrefix(org, fields[0]))
		return asn, provider
	}
	return "", org
}
