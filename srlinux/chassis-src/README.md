# Chassis SVG Build Process

Source SVGs with labeled port shapes live in `chassis-src/`. The production SVGs in `chassis/` are built from these by replacing the full vector artwork with a compressed webp raster, keeping only the port overlay paths.

## Steps to build a chassis SVG

### 1. Label ports in Inkscape

Open the source SVG in Inkscape. Each port element (path or rect) must have its `id` set to the port number (e.g. `1`, `2`, `33`) or `mgmt0`. Save to `chassis-src/`.

### 2. Render to webp

Convert the source SVG to a webp at the viewBox resolution. Check the `viewBox` attribute for the dimensions (e.g. `0 0 1368.5669 123.874` → width 1369, height 124).

```sh
rsvg-convert -w 1369 -h 124 chassis-src/7220-IXR-H5-32D.svg -o /tmp/chassis.png
cwebp -q 80 /tmp/chassis.png -o /tmp/chassis.webp
```

### 3. Build the production SVG

Run the Python script below to combine the webp background with invisible port overlays extracted from the source SVG:

```python
import base64
import xml.etree.ElementTree as ET

with open("/tmp/chassis.webp", "rb") as f:
    b64 = base64.standard_b64encode(f.read()).decode()

tree = ET.parse("chassis-src/7220-IXR-H5-32D.svg")
root = tree.getroot()
ns = 'http://www.w3.org/2000/svg'
vb = root.get('viewBox', '')
vb_w, vb_h = vb.split()[2], vb.split()[3]

port_lines = []
for el in root.iter():
    eid = el.get('id', '')
    if not eid or not (eid.isdigit() or eid.startswith('mgmt')):
        continue
    tag = el.tag.replace(f'{{{ns}}}', '')
    d = el.get('d', '')
    transform = el.get('transform', '')
    if tag == 'path' and d:
        attrs = f'id="{eid}" d="{d}"'
        if transform:
            attrs += f' transform="{transform}"'
        attrs += ' fill="none" stroke="none"'
        port_lines.append(f'  <path {attrs}/>')

lines = []
lines.append('<?xml version="1.0" encoding="UTF-8"?>')
lines.append(f'<svg xmlns="http://www.w3.org/2000/svg" width="500" height="45" viewBox="{vb}">')
lines.append(f'  <image xmlns:xlink="http://www.w3.org/1999/xlink" href="data:image/webp;base64,{b64}" width="{vb_w}" height="{vb_h}" preserveAspectRatio="none"/>')
lines.extend(port_lines)
lines.append('</svg>')

with open("chassis/7220-IXR-H5-32D.svg", "w") as f:
    f.write("\n".join(lines))
```

### 4. Verify

```sh
go build ./...
```

The Go code in `frontpanel.go` uses etree to find the port element by ID and sets `fill` + `opacity` to highlight it. The port paths are invisible (`fill:none; stroke:none`) until highlighted.

## File size targets

| Format | Typical size |
|--------|-------------|
| Original vector SVG | 500KB – 4MB |
| Production webp+overlay SVG | 30KB – 50KB |
