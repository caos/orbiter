package adaptertesting

import (
	"fmt"

	"github.com/golang/mock/gomock"
)

func ExpectValue(testCase string, value interface{}) gomock.Matcher {

	return gomock.GotFormatterAdapter(
		gomock.GotFormatterFunc(func(i interface{}) string {
			return fmt.Sprintf("\x1b[1;31m\"%s\"\x1b[0m\n%+v", testCase, i)
		}),
		gomock.WantFormatter(
			gomock.StringerFunc(func() string { return fmt.Sprintf("\n%+v", value) }),
			gomock.Eq(value),
		),
	)
}
