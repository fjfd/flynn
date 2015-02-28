import Panel from './panel';

var padding = function (str, len) {
	str = ''+ str;
	var n = Math.max(len - str.length, 0);
	for (var i = 0; i < n; i++) {
		str = ' '+ str;
	}
	return str;
};

var NonSelectable = React.createClass({
	render: function () {
		return (
			<span style={{
				WebkitUserSelect: 'none',
				MozUserSelect: 'none',
				msUserSelect: 'none',
				userSelect: 'none'
			}}>
				{this.props.children}
			</span>
		);
	}
});

var InstallProgress = React.createClass({
	render: function () {
		var eventNodes = [];
		var events = this.state.installEvents;
		for (var len = events.length, i = len-1; i >= 0; i--) {
			eventNodes.push(
				<div key={i}>
					<NonSelectable>{padding(i+1, 3)}. </NonSelectable>
					{events[i].description}
				</div>
			);
		}
		return (
			<Panel>
				<pre style={{ width: '100%' }}>
					{eventNodes}
				</pre>
			</Panel>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	__getState: function () {
		return this.props.state;
	}
});
export default InstallProgress;
