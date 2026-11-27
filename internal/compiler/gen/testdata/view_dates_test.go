package sample_test

import (
	"testing"
	"time"

	"example.com/sample/internal/forge_generated/customer"
	"example.com/sample/internal/forge_generated/invoice"
	"github.com/gombit-dev/gombit/types"
)

func TestViewDatesAreImmutable(t *testing.T) {
	c, i := &customer.Customer{}, &invoice.Invoice{}
	cv, iv := customer.NewCustomerView(c), invoice.NewInvoiceView(i)
	if cv.Joined() != nil || iv.DueDate() != nil {
		t.Fatal("expected absent dates to remain nil")
	}
	joined := time.Unix(1000, 0)
	c.Joined = &joined
	i.DueDate = &types.Date{Day: 5}
	if !cv.Joined().Equal(joined) || iv.DueDate().Day != 5 {
		t.Fatal("accessors lost the stored date values")
	}
	*cv.Joined() = time.Time{}
	iv.DueDate().Day = 0
	if c.Joined.Unix() != 1000 || i.DueDate.Day != 5 {
		t.Fatal("mutating a date through a view changed the persistence model")
	}
}
