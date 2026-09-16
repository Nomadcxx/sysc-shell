# Design Preferences

- UI should match prior art (DMS/DankMaterialShell and Noctalia) at minimum, with near pixel-perfect clones where requested. The user states the overall project aim as achieving parity with BOTH shells, and judges it on two dimensions: functional (feature-set coverage vs the reference — e.g. hourly forecasts, city geocoding count as parity gaps) and UI/visual (composition, glyph scale, accent treatment, etc.), not looks alone. Confidence: 0.9
- Each panel element should sit in its own pill/capsule so it reads clearly against the background. Confidence: 0.8
- Panels should attach to the bar with no gap (opening at the top-center of the bar) or open centered on screen like Noctalia/DMS — not float in arbitrary positions. Confidence: 0.75
- Buttons should be pill-shaped, filled where appropriate, with a subtle hover effect. Confidence: 0.75
- Avoids "slop" design; wants UI run through an established design-principles audit (a "hallmark" audit). Confidence: 0.7
- Icons on the bar should be crisp and correctly sized/centered rather than stretched or low-resolution. Confidence: 0.65
- No literal geometry in UI code: spacing comes from the margin ladder, control sizes from the density row, shapes from shape roles; the token-conformance scan (e.g. `TestSurfaceSourcesCarryNoLegacyVisuals`) must stay green after every task that edits the shell. Confidence: 0.8
- Constants that cannot come from the token system (scroll speeds, art box sizes, animation durations) are treated as measured tunables: the design names them with an initial value, and the live gate measures the real number against the reference capture — they are never invented from a document. Confidence: 0.6
- Features can have multiple intentional surfaces: e.g. BT appears embedded inside the control center (which has its own BT elements) AND as a standalone panel opened by right-clicking the BT icon on the bar — both exist and both must be preserved. Confidence: 0.75
