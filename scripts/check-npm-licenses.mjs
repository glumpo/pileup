import { readFileSync } from "node:fs";

const denied = (license) => /GPL|UNKNOWN/i.test(license);

const allowed = (license) => {
  const expression = license.replace(/^\((.*)\)$/, "$1");
  if (/[()]/.test(expression)) return !denied(expression);
  return expression.split(" OR ").some((alternative) => !denied(alternative));
};

const packages = JSON.parse(readFileSync(0, "utf8"));
const violations = Object.entries(packages).filter(([, info]) => ![info.licenses].flat().every(allowed));
for (const [name, info] of violations) console.error(`denied license: ${name} ${info.licenses}`);
process.exit(violations.length > 0 ? 1 : 0);
