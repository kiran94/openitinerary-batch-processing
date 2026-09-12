import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  vus: 1,
  duration: "30s",
  thresholds: {
    http_req_duration: ["p(95) < 500"],
    http_req_failed: ["rate==0"],
  },
};

export default function () {
  const URL = "http://localhost:8080/api/v1/status/";
  const ticketID = __ENV.TICKET_ID;
  if (!ticketID) {
    fail("TICKET_ID must be defined");
  }

  const res = http.get(URL + ticketID);
  check(res, { success: (r) => r.status === 200 });

  sleep(1);
}
