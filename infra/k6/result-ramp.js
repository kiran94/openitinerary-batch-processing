import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  stages: [
    { target: 5, duration: "15s" },
    { target: 5, duration: "30s" },
    { target: 0, duration: "15s" },
  ],
  thresholds: {
    checks: ["rate==1"],
    http_req_failed: ["rate==0"],
    http_req_duration: ["p(95) < 100"],
  },
};

export default function () {
  const URL = "http://localhost:8080/api/v1/result/";
  const ticketID = __ENV.TICKET_ID;
  if (!ticketID) {
    fail("TICKET_ID must be defined");
  }

  const res = http.get(URL + ticketID);
  check(res, { success: (r) => r.status === 200 });

  sleep(1);
}
