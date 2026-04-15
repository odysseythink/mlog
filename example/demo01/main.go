package main

import "fmt"

func SearchV1(val string) string {
	if val == "" {
		return ""
	}

	rbstrs := []string{}
	for iLoop := 0; iLoop < len(val); iLoop++ {
		for jLoop := iLoop + 1; jLoop < len(val); jLoop++ {
			if byte(val[jLoop]) == byte(val[iLoop]) {
				rbstrs = append(rbstrs, val[iLoop:jLoop+1])
			}
		}
	}
	fmt.Println("------", rbstrs)
	maxlen := 0
	maxidx := -1
	for idx, v := range rbstrs {
		if len(v) >= 3 {
			fmt.Println("1111--", v)
			notmatch := false
			for iLoop := 1; iLoop < len(v)-1; iLoop++ {
				fmt.Println(" 111--", string(v[iLoop]), ",first=", string(v[0]))
				if v[iLoop] == v[0] {
					notmatch = true
					break
				}
			}
			if notmatch {
				continue
			}
		}
		if len(v) >= maxlen {
			maxlen = len(v)
			maxidx = idx
		}
	}
	if maxidx >= 0 {
		return rbstrs[maxidx]
	}
	return ""
}

func main() {
	fmt.Println(SearchV1("beaeb"))
}
