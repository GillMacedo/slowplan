# 0002: One npm package with every binary

**Decision:** publish one npm package holding a binary per platform.

**Why:** npm's spam check rejected a platform-specific package in an earlier project, and one package has fewer ways to fail. It's larger to download.
