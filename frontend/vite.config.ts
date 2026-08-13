import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// outDir points at ../web, not the default frontend/dist -- that's what
// server.go's //go:embed web actually bakes into the binary, so `npm run
// build` has to land there directly. Building to the default location and
// forgetting to also pass --outDir was a real, repeated mistake during
// development (the Go binary would compile fine and silently embed
// whatever was already sitting in web/ from a previous build) -- baking
// the right path into the config here means the plain, obvious command is
// also the correct one.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../web',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8098',
    },
  },
})
