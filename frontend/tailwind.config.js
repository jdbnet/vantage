/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{vue,js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        surface: {
          DEFAULT: "#0d1117",
          raised: "#161b22",
          overlay: "#21262d",
        },
        accent: {
          DEFAULT: "rgb(var(--accent) / <alpha-value>)",
          muted: "rgb(var(--accent-muted) / <alpha-value>)",
        },
        slate: {
          200: "#e6edf3",
          300: "#c9d1d9",
          400: "#8b949e",
          500: "#6e7681",
          600: "#484f58",
          700: "#30363d",
          800: "#21262d",
          900: "#161b22",
        },
      },
      fontFamily: {
        sans: ["Space Grotesk", "system-ui", "sans-serif"],
        mono: ["DM Mono", "ui-monospace", "monospace"],
      },
    },
  },
  plugins: [],
};
