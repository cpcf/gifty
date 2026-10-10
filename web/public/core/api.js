async function api(path, data) {
  let r;
  try {
    r = await fetch("/api/" + path, {
      method: data === undefined ? "GET" : "POST",
      headers: data === undefined ? {} : { "Content-Type": "application/json" },
      body: data === undefined ? undefined : JSON.stringify(data),
    });
  } catch {
    throw Error("Can’t reach Gifty. Check your connection and try again.");
  }
  let v;
  try {
    v = await r.json();
  } catch {
    throw Error("The server isn’t responding. Try again in a moment.");
  }
  if (!r.ok) {
    const err = Error(v.error);
    err.status = r.status;
    throw err;
  }
  return v;
}

export { api };
