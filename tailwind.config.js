/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./templates/**/*.html"],
  theme: {
    extend: {
      colors: {
        charcoal: {
          DEFAULT: "#111827",
          light: "#1F2937",
        },
        ivory: {
          DEFAULT: "#FAFAF8",
        },
        emerald: {
          DEFAULT: "#10B981",
          dark: "#059669",
        },
        brand: {
          50: "#ecfdf5",
          100: "#d1fae5",
          500: "#10B981",
          600: "#059669",
          700: "#047857",
        },
      },
    },
  },
  plugins: [],
};
