import http from "k6/http";
import { check } from "k6";

export const options = {
  scenarios: {
    submit_constant: {
      executor: "constant-arrival-rate",
      duration: "10s",
      preAllocatedVUs: 8,
      maxVUs: 10,
      rate: 1,
      timeUnit: "1s",
    },
  },
  thresholds: {
    checks: ["rate==1"],
    http_req_failed: ["rate==0"],
    http_req_duration: ["p(95)<8000"],
    dropped_iterations: ["count==0"],
  },
};

const file = open("./data/test.pdf", "b");

export default function () {
  const URL = "http://localhost:8080/api/v1/submit";
  const data = {
    file: http.file(file, "test.pdf", "application/pdf"),
    notification_emails: JSON.stringify([]),
  };

  const res = http.post(URL, data);

  let body = {};
  try {
    body = res.json();
  } catch {}

  check(
    { response: res, body },
    {
      "status is 202": ({ response }) => response.status === 202,
      "response contains ticket ID": ({ body }) => Boolean(body.id),
      "ticket status is queued": ({ body }) => body.status === "queued",
    },
  );
}
