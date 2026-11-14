package payroll

type employee struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Department   string `json:"department"`
	Manager      string `json:"manager"`
	Started      string `json:"started"`
	PayFrequency string `json:"payFrequency"`
}

type payslip struct {
	ID          string `json:"id"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
	PayDate     string `json:"payDate"`
	Gross       int64  `json:"grossCents"`
	Deductions  []struct {
		Name   string `json:"name"`
		Amount int64  `json:"amountCents"`
	} `json:"deductions"`
	Net int64 `json:"netCents"`
}

type balance struct {
	Type  string  `json:"type"`
	Label string  `json:"label"`
	Hours float64 `json:"hours"`
}

type timeOff struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Start  string  `json:"start"`
	End    string  `json:"end"`
	Hours  float64 `json:"hours"`
	Status string  `json:"status"`
	Note   string  `json:"note"`
}

type bank struct {
	BankName string `json:"bankName"`
	Routing  string `json:"routingNumber"`
	Account  string `json:"accountNumber"`
	Type     string `json:"accountType"`
}

type data struct {
	Employee employee  `json:"employee"`
	Payslips []payslip `json:"payslips"`
	TimeOff  struct {
		Balances []balance  `json:"balances"`
		Requests []*timeOff `json:"requests"`
	} `json:"timeOff"`
	Bank bank `json:"bank"`
}
