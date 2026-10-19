package config

import "testing"

func TestOSSConfigValidation(t *testing.T) {
	c, err := (OSSConfig{Bucket: "weclay", Region: "cn-beijing"}).Resolve()
	if err != nil || c.Endpoint != "https://oss-cn-beijing.aliyuncs.com" || c.Prefix != "core" {
		t.Fatal(c, err)
	}
	for _, endpoint := range []string{"http://example.test", "https://user:pass@example.test", "https://example.test/a", "https://example.test?token=x", "https://example.test#fragment"} {
		bad := c
		bad.Endpoint = endpoint
		if bad.Validate() == nil {
			t.Fatal("accepted invalid endpoint")
		}
	}
	for _, prefix := range []string{"../core", "a/../b", "/core", "core/", "a//b", "a?token=x"} {
		bad := c
		bad.Prefix = prefix
		if bad.Validate() == nil {
			t.Fatal("accepted invalid prefix")
		}
	}
}
