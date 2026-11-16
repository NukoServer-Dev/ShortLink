package consts

import "strings"

var banDomains = []string{
	"discord",
	"fluxer",
	"x.gd",
	"shb.red",
	"nuk.red",
	"localhost",
	"192.168",
	"0.0.0.0",
	"127.0.0.1",
}

func IsInclude(url string) bool {
	for _, domain := range banDomains {
		if strings.Contains(strings.ToLower(url), domain) {
			return true
		}
	}
	return false
}
