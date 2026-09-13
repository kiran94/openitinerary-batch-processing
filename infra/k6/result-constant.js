import http from "k6/http";
import exec from "k6/execution";
import { check, sleep } from "k6";

export const options = {
  discardResponseBodies: true,
  scenarios: {
    status_constant: {
      executor: "constant-arrival-rate",
      duration: "60s",
      preAllocatedVUs: 5,
      maxVUs: 10,
      rate: 5,
      timeUnit: "1s",
    },
  },
  thresholds: {
    checks: ["rate==1"],
    http_req_failed: ["rate==0"],
    http_req_duration: ["p(95)<100"],
  },
};

export default function () {
  const URL = "http://localhost:8080/api/v1/result/";
  const ticketID = __ENV.TICKET_ID;
  if (!ticketID) {
    exec.test.abort("TICKET_ID must be provided");
  }

  const res = http.get(URL + ticketID);
  check(res, { success: (r) => r.status === 200 });
}
