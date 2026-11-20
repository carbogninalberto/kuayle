import base64
import http.client
import ipaddress
import json
import os
import re
import signal
import sys
from contextlib import closing
from email.message import Message
from html.parser import HTMLParser
from urllib.parse import quote, unquote, urlencode, urljoin, urlsplit

MAX_BODY_BYTES = 256 * 1024
MAX_OUTPUT_BYTES = 5500  # Fits in the assistant's 6000-byte custom tool output tail.
TIMEOUT_SECONDS = 15
REDIRECT_STATUSES = {301, 302, 303, 307, 308}
MAX_REDIRECTS = 5
MAX_GITHUB_REF_SEGMENTS = 8
OFFSET_PREFIX = "kuayle-offset="
BLOCKED_HOSTS = {"localhost", "metadata.google.internal", "metadata.google.com"}
URL_SAFE = "/%:@!$&'()*+,;=[]?"
JSON_ESCAPES = str.maketrans({char: f"\\u{ord(char):04x}" for char in "<>&\u2028\u2029"})


class Refusal(Exception):
    pass


class HTTPRefusal(Refusal):
    def __init__(self, status):
        super().__init__(f"Remote server returned HTTP {status}.")
        self.status = status


def https_url(raw):
    if re.search(r"[\s\\\x00-\x1f\x7f]", raw):
        raise Refusal("Invalid URL.")
    url = urlsplit(raw)
    if url.scheme != "https" or not url.hostname:
        raise Refusal("A public HTTPS URL is required.")
    if url.username is not None or url.port not in (None, 443):
        raise Refusal("URL credentials and nonstandard HTTPS ports are unsupported.")
    if len(url._replace(fragment="").geturl().encode()) > 1024:
        raise Refusal("URL is too long.")
    host = url.hostname.rstrip(".")
    if not host or host in BLOCKED_HOSTS or host.endswith(".localhost"):
        raise Refusal("Destination is not public.")
    try:
        ipaddress.ip_address(host)
    except ValueError:
        return url
    # The machine egress proxy only accepts DNS names, including on redirects.
    raise Refusal("A public DNS hostname is required.")


def github_contents_urls(url):
    if url.hostname.rstrip(".") != "github.com":
        return []
    parts = url.path.strip("/").split("/")
    if len(parts) < 2 or (len(parts) > 2 and parts[2] not in ("tree", "blob")):
        return []
    owner, repo = parts[:2]
    if not re.fullmatch(r"[A-Za-z0-9-]+", owner) or not re.fullmatch(r"[A-Za-z0-9_.-]+", repo) or repo in (".", ".."):
        raise Refusal("Invalid GitHub repository URL.")
    api = f"https://api.github.com/repos/{owner}/{repo}/contents"
    if len(parts) == 2:
        return [api]
    if len(parts) < 4 or not parts[3] or (parts[2] == "blob" and len(parts) < 5):
        raise Refusal("GitHub URL must include a ref and, for files, a path.")
    ref_and_path = [unquote(part) for part in parts[3:]]
    if any(not part or part in (".", "..") or "/" in part for part in ref_and_path[1:]):
        raise Refusal("Invalid GitHub content path.")
    # GitHub web links can leave slashes in ref names unescaped. If the first
    # Contents API path is missing, retry with progressively longer refs.
    max_ref = min(MAX_GITHUB_REF_SEGMENTS, len(ref_and_path) - (parts[2] == "blob"))
    urls = []
    for split in range(1, max_ref + 1):
        ref = "/".join(ref_and_path[:split])
        path = "/".join(quote(part, safe="") for part in ref_and_path[split:])
        urls.append(api + ("/" + path if path else "") + "?" + urlencode({"ref": ref}))
    return urls


def download(raw, proxy, accept):
    for hop in range(MAX_REDIRECTS + 1):
        url = https_url(raw)
        target = url.path or "/"
        if url.query:
            target += "?" + url.query
        # Force every request through the machine's CONNECT proxy. It resolves
        # the destination and dials only an address allowed by the forge guard.
        with closing(http.client.HTTPSConnection(proxy.hostname, proxy.port, timeout=TIMEOUT_SECONDS)) as connection:
            connection.set_tunnel(url.hostname.encode("idna").decode(), 443)
            connection.request("GET", quote(target, safe=URL_SAFE), headers={
                "Accept": accept,
                "Accept-Encoding": "identity",
                "User-Agent": "Kuayle-fetch-url",
            })
            with connection.getresponse() as response:
                if response.status in REDIRECT_STATUSES:
                    location = response.getheader("Location")
                    if not location or hop == MAX_REDIRECTS:
                        raise Refusal("Invalid redirect or too many redirects.")
                    raw = urljoin(url.geturl(), location)
                    continue
                if response.status != 200:
                    raise HTTPRefusal(response.status)
                if response.getheader("Content-Encoding", "identity").lower() != "identity":
                    raise Refusal("Unsupported response encoding.")
                length = response.getheader("Content-Length")
                if length is not None and int(length) > MAX_BODY_BYTES:
                    raise Refusal("Response is too large.")
                body = response.read(MAX_BODY_BYTES + 1)
                if len(body) > MAX_BODY_BYTES:
                    raise Refusal("Response is too large.")
                return body, response.getheader("Content-Type", "text/plain"), url.geturl()


class PageText(HTMLParser):
    hidden = {"script", "style", "template"}
    blocks = {"article", "br", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "p", "pre", "section", "tr"}

    def __init__(self):
        super().__init__()
        self.parts = []
        self.skip = []

    def handle_starttag(self, tag, attrs):
        if tag in self.hidden:
            self.skip.append(tag)
        if not self.skip and tag in self.blocks:
            self.parts.append("\n")

    def handle_endtag(self, tag):
        if self.skip and tag == self.skip[-1]:
            self.skip.pop()
        if not self.skip and tag in self.blocks:
            self.parts.append("\n")

    def handle_data(self, data):
        if not self.skip:
            self.parts.append(data)

    def text(self):
        lines = (" ".join(line.split()) for line in "".join(self.parts).splitlines())
        return "\n".join(line for line in lines if line)


def github_text(body):
    try:
        data = json.loads(body)
        if data["type"] == "dir":
            return "\n".join(
                f'{entry["type"]} {entry["path"]} {entry["html_url"] or ""}'.rstrip()
                for entry in data["entries"]
            )
        if data["type"] == "file" and data["encoding"] == "base64":
            return base64.b64decode(data["content"]).decode("utf-8")
    except (KeyError, TypeError, ValueError):
        pass
    raise Refusal("GitHub did not return a readable text file or directory.")


def page_text(body, content_type):
    header = Message()
    header["Content-Type"] = content_type
    try:
        text = body.decode(header.get_content_charset() or "utf-8")
    except (LookupError, UnicodeError) as error:
        raise Refusal("Response is not readable text.") from error
    if "\x00" in text:
        raise Refusal("Response is not readable text.")
    if header.get_content_type() in ("text/html", "application/xhtml+xml"):
        parser = PageText()
        parser.feed(text)
        parser.close()
        return parser.text()
    return text


def encode(result):
    return json.dumps(result, ensure_ascii=False, separators=(",", ":")).translate(JSON_ESCAPES)


def page_result(content, source, effective_url, offset):
    if offset > len(content):
        raise Refusal("Continuation is past the end of the text.")
    data = {"url": effective_url, "content": content[offset:offset + MAX_OUTPUT_BYTES]}
    result = {"untrusted_data": data}
    if len(content) - offset <= MAX_OUTPUT_BYTES and len(encode(result).encode()) <= MAX_OUTPUT_BYTES:
        return result

    # Measure the JSON representation so escaping and UTF-8 cannot cause the
    # assistant's output tail to cut off the untrusted-data marker or next URL.
    start, end = offset + 1, min(len(content), offset + MAX_OUTPUT_BYTES)
    while start < end:
        middle = (start + end + 1) // 2
        data["content"] = content[offset:middle]
        data["next_url"] = source._replace(fragment=f"{OFFSET_PREFIX}{middle}").geturl()
        if len(encode(result).encode()) <= MAX_OUTPUT_BYTES:
            start = middle
        else:
            end = middle - 1
    data["content"] = content[offset:start]
    data["next_url"] = source._replace(fragment=f"{OFFSET_PREFIX}{start}").geturl()
    if len(encode(result).encode()) > MAX_OUTPUT_BYTES:
        raise Refusal("Result is too large to return with this URL.")
    return result


def fetch_url(raw, proxy_url):
    source = https_url(raw)
    offset = 0
    if source.fragment.startswith(OFFSET_PREFIX):
        value = source.fragment.removeprefix(OFFSET_PREFIX)
        if not re.fullmatch(r"[0-9]{1,7}", value):
            raise Refusal("Invalid continuation URL.")
        offset = int(value)
    proxy = urlsplit(proxy_url)
    if proxy.scheme != "http" or not proxy.hostname or not proxy.port or proxy.username is not None:
        raise Refusal("The Dev Machine egress proxy is required.")

    targets = github_contents_urls(source)
    if not targets:
        body, content_type, effective_url = download(source.geturl(), proxy, "*/*")
        content = page_text(body, content_type)
        return page_result(content, source, urlsplit(effective_url)._replace(fragment="").geturl(), offset)

    for target in targets:
        try:
            body, _, _ = download(target, proxy, "application/vnd.github.object+json")
        except HTTPRefusal as error:
            if error.status == 404 and target != targets[-1]:
                continue
            raise
        return page_result(github_text(body), source, source._replace(fragment="").geturl(), offset)


def timeout(*_):
    raise TimeoutError()


def main():
    signal.signal(signal.SIGALRM, timeout)
    signal.alarm(TIMEOUT_SECONDS)
    try:
        print(encode(fetch_url(os.environ.get("KUAYLE_ARG_URL", ""), os.environ.get("HTTPS_PROXY", ""))))
    except TimeoutError:
        sys.exit("URL fetch timed out.")
    except Refusal as error:
        sys.exit(str(error))
    except (OSError, http.client.HTTPException, ValueError):
        sys.exit("URL could not be fetched. Check the URL and the Dev Machine egress policy.")
    finally:
        signal.alarm(0)


if __name__ == "__main__":
    main()
