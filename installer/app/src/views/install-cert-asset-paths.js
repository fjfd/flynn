var AssetPaths = {};
[
	"windows-firefox-cert-1.png",
	"linux-firefox-cert-1.png",
	"osx-firefox-cert-1.png",
	"osx-cert-2.png",
	"osx-cert-2.png",
	"linux-chrome-cert-1.png",
	"linux-chrome-cert-2.png",
	"linux-chrome-cert-3.png",
	"linux-chrome-cert-4.png",
	"linux-chrome-cert-5.png",
	"windows-cert-1.png",
	"windows-cert-2.png",
	"windows-cert-3.png",
	"windows-cert-4.png",
	"windows-cert-5.png",
	"windows-cert-6.png",
	"windows-cert-7.png",
].forEach(function (name) {
	AssetPaths[name] = '/assets/'+ name;
});
export default AssetPaths;
