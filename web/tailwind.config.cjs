/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: [
          '-apple-system',
          'BlinkMacSystemFont',
          '"SF Pro Text"',
          '"SF Pro Display"',
          '"PingFang SC"',
          '"Hiragino Sans GB"',
          '"Helvetica Neue"',
          '"Microsoft YaHei UI"',
          '"Segoe UI"',
          'system-ui',
          'sans-serif',
        ],
        mono: [
          '"SF Mono"',
          'Menlo',
          'Monaco',
          'Consolas',
          '"Liberation Mono"',
          '"Courier New"',
          'monospace',
        ],
      },
      colors: {
        slate: {
          950: '#161618',
          900: '#1f1f22',
          850: '#26262a',
          800: '#2e2e33',
          700: '#3d3d44',
          600: '#54545e',
          500: '#767682',
          400: '#9e9ea9',
          300: '#cacad1',
          200: '#e4e4e9',
          100: '#f4f4f7',
        },
        indigo: {
          950: '#0d2542',
          900: '#12345c',
          800: '#164982',
          700: '#0062cc',
          600: '#0a84ff',
          500: '#2492ff',
          400: '#52a9ff',
          300: '#82c0ff',
        },
      },
    },
  },
  plugins: [],
};
