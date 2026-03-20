const esbuild = require("esbuild");

const isWatch = process.argv.includes("--watch");

const config = {
  entryPoints: ["src/app.jsx"],
  bundle: true,
  outfile: "dist/app.js",
  minify: !isWatch,
  sourcemap: isWatch,
  jsxFactory: "h",
  jsxFragment: "Fragment",
  define: {
    "process.env.NODE_ENV": isWatch ? '"development"' : '"production"',
  },
  loader: { ".jsx": "jsx" },
};

if (isWatch) {
  esbuild
    .context(config)
    .then((ctx) => {
      ctx.watch();
      console.log("Watching for changes...");
    })
    .catch(() => process.exit(1));
} else {
  esbuild.build(config).catch(() => process.exit(1));
}
