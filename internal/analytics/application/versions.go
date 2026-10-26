package application

import (
	"regexp"
	"strings"
)

// browserVersion is the browser's major version from its own token (Change 35). Safari carries
// its version in "Version/", Chrome and Firefox on iOS in CriOS/ and FxiOS/.
var browserVersion = map[string]*regexp.Regexp{
	"Yandex Browser": regexp.MustCompile(`yabrowser/(\d+)`),
	"Edge":           regexp.MustCompile(`edg(?:a|ios)?/(\d+)`),
	"Opera":          regexp.MustCompile(`(?:opr|opera)/(\d+)`),
	"Firefox":        regexp.MustCompile(`(?:firefox|fxios)/(\d+)`),
	"Chrome":         regexp.MustCompile(`(?:chrome|crios)/(\d+)`),
	"Safari":         regexp.MustCompile(`version/(\d+)`),
}

var (
	windowsNT  = regexp.MustCompile(`windows nt (\d+\.\d+)`)
	androidVer = regexp.MustCompile(`android (\d+)`)
	iosVer     = regexp.MustCompile(`(?:iphone|cpu) os (\d+)`)
	macVer     = regexp.MustCompile(`mac os x (\d+)[._](\d+)`)
)

// windowsNames maps the NT version a Windows UA still carries; Windows 11 says "NT 10.0" too.
var windowsNames = map[string]string{"10.0": "10/11", "6.3": "8.1", "6.2": "8", "6.1": "7"}

// uaVersions derives the browser major version and the OS version a User-Agent still carries
// (Change 35). Chromium freezes the macOS version at 10.15 and Android at 10 in reduced UAs, so the
// OS version is a hint, not a fact; an unknown one is "".
func uaVersions(ua, browser, os string) (browserMajor, osVersion string) {
	ual := strings.ToLower(ua)
	if re, ok := browserVersion[browser]; ok {
		if m := re.FindStringSubmatch(ual); m != nil {
			browserMajor = m[1]
		}
	}
	switch os {
	case "Windows":
		if m := windowsNT.FindStringSubmatch(ual); m != nil {
			osVersion = windowsNames[m[1]]
		}
	case "Android":
		if m := androidVer.FindStringSubmatch(ual); m != nil {
			osVersion = m[1]
		}
	case "iOS":
		if m := iosVer.FindStringSubmatch(ual); m != nil {
			osVersion = m[1]
		}
	case "macOS":
		if m := macVer.FindStringSubmatch(ual); m != nil {
			osVersion = m[1] + "." + m[2]
		}
	}
	return browserMajor, osVersion
}
