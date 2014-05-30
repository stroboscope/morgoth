#
# Copyright 2014 Nathaniel Cook
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


from flask import Flask, request, jsonify, make_response, current_app, render_template, redirect
from gevent.pywsgi import WSGIServer
from morgoth.fittings.fitting import Fitting
from morgoth.fittings.dashboard.forms import CheckMetric

import logging
logger = logging.getLogger(__name__)

app = Flask(__name__)

class Dashboard(Fitting):
    """
    Dashboard fitting that provides a web front end to the
    metrics and anomalies in morgoth
    """
    def __init__(self, morgoth_app, host, port):
        super(Dashboard, self).__init__()
        self._morgoth_app = morgoth_app
        self._host = host
        self._port = port
        self._server = None


    @classmethod
    def from_conf(cls, conf, morgoth_app):
        host = ''
        port = conf.get('port', 8080)
        return Dashboard(morgoth_app, host, port)


    def start(self):
        logger.info("Starting Dashboard fitting...")
        self._server = WSGIServer((self._host, self._port), app, log=None)
        self._server.serve_forever()

    def stop(self):
        self._server.stop()

##############################
# Flask methods
##############################


@app.route('/')
def root():
    """
    Root for the Dashboard
    """
    return app.send_static_file('dashboard.html')


@app.route('/check_metric', methods=['GET', 'POST'])
def check_metric():
    """
    HTML Form for submitting a single check for anomalies on a metric
    """
    form = CheckMetric(request.form)
    if request.method == 'POST':
        return redirect('/check_metric')
    return render_template('check_metric.html', form=form)
